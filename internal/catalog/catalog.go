// Package catalog owns the provider/model/region resource catalog
// (resource_*_catalog tables).
package catalog

// ResourceCatalogView is the tenant-visible resource catalog.
type ResourceCatalogView struct {
	Providers []ProviderCatalogItem `json:"providers"`
	Models    []ModelCatalogItem    `json:"models"`
	Regions   []RegionCatalogItem   `json:"regions"`
}

type ProviderCatalogItem struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	Endpoint string `json:"endpoint"`
}

type ModelCatalogItem struct {
	ID            string   `json:"id"`
	ProviderID    string   `json:"providerId"`
	Model         string   `json:"model"`
	Capabilities  []string `json:"capabilities"`
	ContextWindow *int     `json:"contextWindow,omitempty"`
}

type RegionCatalogItem struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}
