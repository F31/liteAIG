package openai

import "github.com/F31/liteAIG/internal/kernel/runtime"

type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}
type ModelsResponse struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

func ListModels(snapshot *runtime.TenantRuntimeSnapshot, key runtime.APIKey) ModelsResponse {
	allowed := make(map[string]bool, len(key.ModelAllowlist))
	for _, name := range key.ModelAllowlist {
		allowed[name] = true
	}
	response := ModelsResponse{Object: "list"}
	for _, item := range snapshot.LogicalModels() {
		if len(allowed) > 0 && !allowed[item.Alias] {
			continue
		}
		response.Data = append(response.Data, Model{ID: item.Alias, Object: "model", Created: snapshot.PublishedAt.Unix(), OwnedBy: "liteaig"})
	}
	return response
}
