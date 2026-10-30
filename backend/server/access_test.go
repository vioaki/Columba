package server

import (
	"strings"
	"testing"
)

// 回归 #1/#7：纯中文/纯符号名称清洗后为空时，slugID 必须回退到非空 id，
// 否则存储层会误报 "id is required"。
func TestSlugIDChineseAndSymbols(t *testing.T) {
	cases := []string{"源", "我的模型组", "测试 source", "！@￥", "  "}
	for _, name := range cases {
		got := slugID(name)
		if strings.TrimSpace(got) == "" {
			t.Fatalf("slugID(%q) returned empty, want non-empty fallback", name)
		}
	}
	// 正常英文名仍应得到可读 slug。
	if got := slugID("OpenAI Main"); got != "openai-main" {
		t.Fatalf("slugID(\"OpenAI Main\") = %q, want openai-main", got)
	}
	// 中英混合：保留英文数字部分。
	if got := slugID("源gpt4"); got != "gpt4" {
		t.Fatalf("slugID(\"源gpt4\") = %q, want gpt4", got)
	}
}
