// 模型与分组的查询/写入：模型列清单、分组 CRUD 与 token-分组引用同步。
package storage

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// modelColumns 是 models 行读取的统一列清单（含 enabled/origin/capability_source）。
const modelColumns = `m.id, m.source_id, m.name, m.source_name, m.base_url, m.api_key, m.platform, m.type, m.max_tokens, m.vision_capable, m.tools_capable, m.structured_output, m.thinking_mode, m.available, m.enabled, m.origin, m.capability_source, m.last_checked_at`

// scanModel 把一行 models 数据扫进 Model（解密 api_key）。
func (s *Store) scanModel(scanner interface{ Scan(dest ...any) error }) (Model, error) {
	var item Model
	var vision, tools, structured, available, enabled int
	var checked string
	if err := scanner.Scan(&item.ID, &item.SourceID, &item.Name, &item.SourceName, &item.BaseURL, &item.APIKey, &item.Platform, &item.Type, &item.MaxTokens, &vision, &tools, &structured, &item.ThinkingMode, &available, &enabled, &item.Origin, &item.CapabilitySource, &checked); err != nil {
		return Model{}, err
	}
	item.VisionCapable = sqlIntToBool(vision)
	item.ToolsCapable = sqlIntToBool(tools)
	item.StructuredOutput = sqlIntToBool(structured)
	item.Available = sqlIntToBool(available)
	item.Enabled = sqlIntToBool(enabled)
	item.LastCheckedAt = parseTime(checked)
	item.APIKey = s.decryptOrClear("model api_key", item.ID+"/"+item.SourceID, item.APIKey)
	return item, nil
}

func (s *Store) ListModels(ctx context.Context) ([]Model, error) {
	return s.listModelsFiltered(ctx, ModelListFilter{})
}

// ListModelsFiltered 按过滤条件列出模型（管理面检索，方向4）。
func (s *Store) ListModelsFiltered(ctx context.Context, filter ModelListFilter) ([]Model, error) {
	return s.listModelsFiltered(ctx, filter)
}

func (s *Store) listModelsFiltered(ctx context.Context, filter ModelListFilter) ([]Model, error) {
	where := "WHERE ms.enabled = 1"
	if filter.ShouldIncludeDisabledSources {
		where = "WHERE 1=1"
	}
	args := []any{}
	if filter.SourceID != "" {
		where += " AND m.source_id = ?"
		args = append(args, filter.SourceID)
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		where += " AND (m.id LIKE ? ESCAPE '\\' OR m.name LIKE ? ESCAPE '\\' OR m.source_name LIKE ? ESCAPE '\\')"
		like := "%" + escapeLike(search) + "%"
		args = append(args, like, like, like)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+modelColumns+` FROM models m JOIN model_sources ms ON m.source_id = ms.id `+where+` ORDER BY m.source_name, m.name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Model{}
	for rows.Next() {
		item, err := s.scanModel(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// escapeLike 转义 LIKE 通配符，保证搜索词按字面匹配。
func escapeLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

// UpdateModel 按 (id, source_id) 更新单个模型的可编辑字段，返回是否找到该行。
// 任一能力字段（vision/tools/structured/thinking/maxTokens/type）被修改时，
// capability_source 置为 'manual'——后续刷新保留这些用户修改值。
func (s *Store) UpdateModel(ctx context.Context, modelID, sourceID string, patch ModelPatch) (bool, error) {
	sets := []string{}
	args := []any{}
	capabilityTouched := false
	if patch.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *patch.Name)
	}
	if patch.Type != nil {
		sets = append(sets, "type = ?")
		args = append(args, *patch.Type)
		capabilityTouched = true
	}
	if patch.MaxTokens != nil {
		sets = append(sets, "max_tokens = ?")
		args = append(args, *patch.MaxTokens)
		capabilityTouched = true
	}
	if patch.VisionCapable != nil {
		sets = append(sets, "vision_capable = ?")
		args = append(args, sqlBoolToInt(*patch.VisionCapable))
		capabilityTouched = true
	}
	if patch.ToolsCapable != nil {
		sets = append(sets, "tools_capable = ?")
		args = append(args, sqlBoolToInt(*patch.ToolsCapable))
		capabilityTouched = true
	}
	if patch.StructuredOutput != nil {
		sets = append(sets, "structured_output = ?")
		args = append(args, sqlBoolToInt(*patch.StructuredOutput))
		capabilityTouched = true
	}
	if patch.ThinkingMode != nil {
		sets = append(sets, "thinking_mode = ?")
		args = append(args, *patch.ThinkingMode)
		capabilityTouched = true
	}
	if patch.Enabled != nil {
		sets = append(sets, "enabled = ?")
		args = append(args, sqlBoolToInt(*patch.Enabled))
	}
	if len(sets) == 0 {
		// 无字段更新：探测行是否存在即可。
		var one int
		err := s.db.QueryRowContext(ctx, `SELECT 1 FROM models WHERE id = ? AND source_id = ?`, modelID, sourceID).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return err == nil, err
	}
	if capabilityTouched {
		sets = append(sets, "capability_source = 'manual'")
	}
	args = append(args, modelID, sourceID)
	res, err := s.db.ExecContext(ctx, `UPDATE models SET `+strings.Join(sets, ", ")+` WHERE id = ? AND source_id = ?`, args...)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// DeleteModel 删除单个模型并清理组内引用（事务化防悬空）。返回是否删除了行。
func (s *Store) DeleteModel(ctx context.Context, modelID, sourceID string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM models WHERE id = ? AND source_id = ?`, modelID, sourceID)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, nil
	}
	return true, tx.Commit()
}

// ModelListFilter 提供管理面的模型列表过滤（方向4）：SourceID 为空查全部；
// Search 对 id/name/source_name 做 SQL LIKE 模糊匹配（已 escape 通配符）。
type ModelListFilter struct {
	SourceID string
	Search   string
	// ShouldIncludeDisabledSources is used by whole-database migrations, which
	// must preserve contracts even for sources that are currently disabled.
	ShouldIncludeDisabledSources bool
}

// ModelPatch 是单个模型的部分更新（方向4）：nil 字段表示不修改。
type ModelPatch struct {
	Name             *string
	Type             *string
	MaxTokens        *int
	VisionCapable    *bool
	ToolsCapable     *bool
	StructuredOutput *bool
	ThinkingMode     *string
	Enabled          *bool
}
