package sqlrepo

import (
	"context"
	"database/sql"

	"github.com/F31/liteAIG/internal/catalog"
)

type ResourceCatalogRepository struct{ db *sql.DB }

func NewResourceCatalogRepository(db *sql.DB) *ResourceCatalogRepository {
	return &ResourceCatalogRepository{db: db}
}

func (r *ResourceCatalogRepository) Catalog(ctx context.Context) (catalog.ResourceCatalogView, error) {
	providers, err := r.providers(ctx)
	if err != nil {
		return catalog.ResourceCatalogView{}, err
	}
	models, err := r.models(ctx)
	if err != nil {
		return catalog.ResourceCatalogView{}, err
	}
	regions, err := r.regions(ctx)
	if err != nil {
		return catalog.ResourceCatalogView{}, err
	}
	return catalog.ResourceCatalogView{Providers: providers, Models: models, Regions: regions}, nil
}

func (r *ResourceCatalogRepository) providers(ctx context.Context) ([]catalog.ProviderCatalogItem, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, label, provider_type, endpoint FROM resource_provider_catalog WHERE status='active' ORDER BY sort_order, label`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []catalog.ProviderCatalogItem
	for rows.Next() {
		var item catalog.ProviderCatalogItem
		if err := rows.Scan(&item.ID, &item.Label, &item.Type, &item.Endpoint); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *ResourceCatalogRepository) regions(ctx context.Context) ([]catalog.RegionCatalogItem, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, label FROM resource_region_catalog WHERE status='active' ORDER BY sort_order, label`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []catalog.RegionCatalogItem
	for rows.Next() {
		var item catalog.RegionCatalogItem
		if err := rows.Scan(&item.ID, &item.Label); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *ResourceCatalogRepository) models(ctx context.Context) ([]catalog.ModelCatalogItem, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, provider_id, model, capabilities, context_window FROM resource_model_catalog WHERE status='active' ORDER BY sort_order, model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []catalog.ModelCatalogItem
	for rows.Next() {
		var item catalog.ModelCatalogItem
		var capabilities string
		var contextWindow sql.NullInt64
		if err := rows.Scan(&item.ID, &item.ProviderID, &item.Model, &capabilities, &contextWindow); err != nil {
			return nil, err
		}
		decodeJSON([]byte(capabilities), &item.Capabilities)
		if contextWindow.Valid {
			value := int(contextWindow.Int64)
			item.ContextWindow = &value
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
