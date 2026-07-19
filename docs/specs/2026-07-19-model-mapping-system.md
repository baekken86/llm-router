# Model Mapping System

Replace the `@cf/` prefix-strip alias (ADR-006) with a user-managed model-to-model mapping UI. Users can declare that one model is identical to another, inheriting its metadata via live reference.

## Problem

Cloudflare model IDs (`@cf/meta/llama-3.1-8b-instruct`) don't match canonical names in `models.json` (`llama-3.1-8b-instruct`). The previous approach was a code-level prefix-strip. This is fragile (doesn't handle suffix differences like `-it`, `-instruct`) and invisible to users — no way to see or correct mismatches.

## Solution: Model Mappings

A mapping declares that source model's metadata is entirely replaced by target model's metadata. The source's own tags are never consulted while mapped.

### 1. Data Model

New table `model_mappings`:

```sql
CREATE TABLE model_mappings (
    source_model_id INTEGER PRIMARY KEY REFERENCES models(id) ON DELETE CASCADE,
    target_model_id INTEGER NOT NULL REFERENCES models(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
```

- One source → one target (enforced by PK)
- Target can have many sources (no unique on target)
- CASCADE delete: removing either model removes the mapping

### 2. Repository

```go
type ModelMappingRepository interface {
    Set(ctx, sourceModelID, targetModelID int64) error
    Get(ctx, sourceModelID int64) (*ModelMapping, error)
    GetAll(ctx) ([]ModelMapping, error)
    Delete(ctx, sourceModelID int64) error
}
```

Additional query needed: `GetAllJoined()` — returns mappings with source/target model name + provider name for the Mappings tab.

### 3. Resolution Integration

In `virual_model_service.go`, `resolveModelsFiltered()`:

When resolving a model, check for a mapping. If mapped:
- Ignore `model_tags` of source model entirely
- Use `model_tags` from target model
- Use `model_metadata_global` for target model's canonical name
- Provider identity stays as source (the model is still served under its own provider)

This is a live reference — remapping or changing target's tags immediately affects resolution.

Pseudo:

```go
mapping, _ := s.mappingRepo.Get(ctx, m.ID)
if mapping != nil {
    tags, _ = s.tagRepo.GetByModelEffort(ctx, mapping.TargetModelID, effort)
    targetModel, _ := s.modelRepo.GetByID(ctx, mapping.TargetModelID)
    if targetModel != nil {
        globalMeta, _ = s.globalMetaRepo.GetByModelEffort(ctx, targetModel.Name, effort)
    }
} else {
    tags, _ = s.tagRepo.GetByModelEffort(ctx, m.ID, effort)
    globalMeta, _ = s.globalMetaRepo.GetByModelEffort(ctx, m.Name, effort)
}
```

### 4. API Endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/v1/models/{id}/mapping` | Create/update mapping (body: `{"target_model_id": ...}`) |
| `GET` | `/api/v1/models/{id}/mapping` | Get mapping for a model |
| `DELETE` | `/api/v1/models/{id}/mapping` | Remove mapping |
| `GET` | `/api/v1/mappings` | List all mappings with source/target names and providers |

`GET /api/v1/mappings` response:
```json
[
    {
        "source_model_id": 1,
        "source_model_name": "@cf/zai-org/glm-5.2",
        "source_provider_name": "cloudflare",
        "target_model_id": 42,
        "target_model_name": "glm-5.2",
        "target_provider_name": "opencode-zen",
        "created_at": "2026-07-19 21:00:00"
    }
]
```

Also extend `GET /api/v1/models` response to include optional mapping fields per model:

```json
{
    "id": 1,
    "name": "@cf/zai-org/glm-5.2",
    "provider_id": 8,
    "provider_name": "cloudflare",
    "tags": [...],
    "mapping_target_id": 42,
    "mapping_target_name": "glm-5.2"
}
```

### 5. TUI

A new **Mappings** tab (between Log and Syslog? Or after Models?). The tab shows a table of all mappings:

| Source Model | Source Provider | → | Target Model | Target Provider | Created | Action |
|---|---|---|---|---|---|---|
| @cf/zai-org/glm-5.2 | cloudflare | → | glm-5.2 | opencode-zen | 2026-07-19 | Delete |

In **Models > Raw sub-tab**:
- Each model row shows `[→ TargetName]` next to name if mapped
- Press `m` on a model: opens a filterable model picker overlay
  - Text input for filtering by name
  - Results grouped by provider
  - Navigate with arrows, Enter to select target → create mapping
- Press `M` on a mapped model: removes the mapping

### 6. Web UI

New **Mappings** tab component (`MappingsTab.svelte`):
- Table with columns: Source Model, Source Provider, Target Model, Target Provider, Created, Actions (Delete button)
- Delete triggers confirm dialog → `DELETE /api/v1/models/{id}/mapping`
- Search/filter bar for source or target name

`RawModelList.svelte` changes:
- Action button (two-arrows icon) per model row → opens searchable model picker modal
- Model picker shows all models grouped by provider, filtered by search input
- Click target → `POST /api/v1/models/{sourceId}/mapping`
- If already mapped: show `→ TargetName` badge with X to unmap

New component `ModelPicker.svelte`:
- Search input, filtered model list grouped by provider
- Props: `excludeModelId`, `onSelect(modelId)`
- Reusable for TUI and Web UI (Web only for now)

### 7. Migration

New migration `020_add_model_mappings.sql`.

### 8. Cleanup

Remove `stripCFPrefix` from `global_metadata_repo.go`. ADR-006 superseded — the alias approach is replaced by user-managed mappings.

Keep ADR-006's model list change in `setup.go` (the 11 CF models) — that's still correct.

## Rejected Alternatives

- **Prefix-strip alias (ADR-006)**: Fragile, doesn't handle suffix differences (`-it`, `-instruct`), invisible to users.
- **Snapshot at mapping time**: User wanted live reference so target metadata changes propagate.
- **Auto-discovery of matching canonical names**: Heuristic, wrong matches possible.
