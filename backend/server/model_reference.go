package server

import (
	"columba.local/backend/config"
	"columba.local/backend/storage"
)

func modelReference(model storage.Model) config.ModelRef {
	return config.ModelRef{ID: model.ID, Name: model.Name, SourceID: model.SourceID, BaseURL: model.BaseURL, APIKey: model.APIKey, Platform: model.Platform, ToolsCapable: model.ToolsCapable, VisionCapable: model.VisionCapable}
}
