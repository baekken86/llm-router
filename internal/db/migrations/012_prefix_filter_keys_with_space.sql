-- +goose Up
UPDATE virtual_models SET filter_expr = REPLACE(filter_expr, '"key": "intelligence"', '"key": "mc.intelligence"') WHERE filter_expr LIKE '%"key": "intelligence"%';
UPDATE virtual_models SET filter_expr = REPLACE(filter_expr, '"key":"intelligence"', '"key":"mc.intelligence"') WHERE filter_expr LIKE '%"key":"intelligence"%';
UPDATE virtual_models SET filter_expr = REPLACE(filter_expr, '"key": "hallucination"', '"key": "mc.hallucination"') WHERE filter_expr LIKE '%"key": "hallucination"%';
UPDATE virtual_models SET filter_expr = REPLACE(filter_expr, '"key":"hallucination"', '"key":"mc.hallucination"') WHERE filter_expr LIKE '%"key":"hallucination"%';
UPDATE virtual_models SET filter_expr = REPLACE(filter_expr, '"key": "coding"', '"key": "mc.coding"') WHERE filter_expr LIKE '%"key": "coding"%';
UPDATE virtual_models SET filter_expr = REPLACE(filter_expr, '"key":"coding"', '"key":"mc.coding"') WHERE filter_expr LIKE '%"key":"coding"%';
UPDATE virtual_models SET filter_expr = REPLACE(filter_expr, '"key": "cost_per_task"', '"key": "mc.cost_per_task"') WHERE filter_expr LIKE '%"key": "cost_per_task"%';
UPDATE virtual_models SET filter_expr = REPLACE(filter_expr, '"key":"cost_per_task"', '"key":"mc.cost_per_task"') WHERE filter_expr LIKE '%"key":"cost_per_task"%';
UPDATE virtual_models SET filter_expr = REPLACE(filter_expr, '"key": "cost_type"', '"key": "p.billing_type"') WHERE filter_expr LIKE '%"key": "cost_type"%';
UPDATE virtual_models SET filter_expr = REPLACE(filter_expr, '"key":"cost_type"', '"key":"p.billing_type"') WHERE filter_expr LIKE '%"key":"cost_type"%';
UPDATE virtual_models SET sort_expr = REPLACE(sort_expr, '"key": "intelligence"', '"key": "mc.intelligence"') WHERE sort_expr LIKE '%"key": "intelligence"%';
UPDATE virtual_models SET sort_expr = REPLACE(sort_expr, '"key":"intelligence"', '"key":"mc.intelligence"') WHERE sort_expr LIKE '%"key":"intelligence"%';
UPDATE virtual_models SET sort_expr = REPLACE(sort_expr, '"key": "coding"', '"key": "mc.coding"') WHERE sort_expr LIKE '%"key": "coding"%';
UPDATE virtual_models SET sort_expr = REPLACE(sort_expr, '"key":"coding"', '"key":"mc.coding"') WHERE sort_expr LIKE '%"key":"coding"%';
UPDATE virtual_models SET sort_expr = REPLACE(sort_expr, '"key": "cost_per_task"', '"key": "mc.cost_per_task"') WHERE sort_expr LIKE '%"key": "cost_per_task"%';
UPDATE virtual_models SET sort_expr = REPLACE(sort_expr, '"key":"cost_per_task"', '"key":"mc.cost_per_task"') WHERE sort_expr LIKE '%"key":"cost_per_task"%';

-- +goose Down
UPDATE virtual_models SET filter_expr = REPLACE(filter_expr, '"key": "mc.intelligence"', '"key": "intelligence"') WHERE filter_expr LIKE '%"key": "mc.intelligence"%';
UPDATE virtual_models SET filter_expr = REPLACE(filter_expr, '"key":"mc.intelligence"', '"key":"intelligence"') WHERE filter_expr LIKE '%"key":"mc.intelligence"%';
UPDATE virtual_models SET filter_expr = REPLACE(filter_expr, '"key": "mc.hallucination"', '"key": "hallucination"') WHERE filter_expr LIKE '%"key": "mc.hallucination"%';
UPDATE virtual_models SET filter_expr = REPLACE(filter_expr, '"key":"mc.hallucination"', '"key":"hallucination"') WHERE filter_expr LIKE '%"key":"mc.hallucination"%';
UPDATE virtual_models SET filter_expr = REPLACE(filter_expr, '"key": "mc.coding"', '"key": "coding"') WHERE filter_expr LIKE '%"key": "mc.coding"%';
UPDATE virtual_models SET filter_expr = REPLACE(filter_expr, '"key":"mc.coding"', '"key":"coding"') WHERE filter_expr LIKE '%"key":"mc.coding"%';
UPDATE virtual_models SET filter_expr = REPLACE(filter_expr, '"key": "p.billing_type"', '"key": "cost_type"') WHERE filter_expr LIKE '%"key": "p.billing_type"%';
UPDATE virtual_models SET filter_expr = REPLACE(filter_expr, '"key":"p.billing_type"', '"key": "cost_type"') WHERE filter_expr LIKE '%"key":"p.billing_type"%';
UPDATE virtual_models SET filter_expr = REPLACE(filter_expr, '"key": "mc.cost_per_task"', '"key": "cost_per_task"') WHERE filter_expr LIKE '%"key": "mc.cost_per_task"%';
UPDATE virtual_models SET filter_expr = REPLACE(filter_expr, '"key":"mc.cost_per_task"', '"key":"cost_per_task"') WHERE filter_expr LIKE '%"key":"mc.cost_per_task"%';
UPDATE virtual_models SET sort_expr = REPLACE(sort_expr, '"key": "mc.intelligence"', '"key": "intelligence"') WHERE sort_expr LIKE '%"key": "mc.intelligence"%';
UPDATE virtual_models SET sort_expr = REPLACE(sort_expr, '"key":"mc.intelligence"', '"key":"intelligence"') WHERE sort_expr LIKE '%"key":"mc.intelligence"%';
UPDATE virtual_models SET sort_expr = REPLACE(sort_expr, '"key": "mc.coding"', '"key": "coding"') WHERE sort_expr LIKE '%"key": "mc.coding"%';
UPDATE virtual_models SET sort_expr = REPLACE(sort_expr, '"key":"mc.coding"', '"key":"coding"') WHERE sort_expr LIKE '%"key":"mc.coding"%';
UPDATE virtual_models SET sort_expr = REPLACE(sort_expr, '"key": "mc.cost_per_task"', '"key": "cost_per_task"') WHERE sort_expr LIKE '%"key": "mc.cost_per_task"%';
UPDATE virtual_models SET sort_expr = REPLACE(sort_expr, '"key":"mc.cost_per_task"', '"key":"cost_per_task"') WHERE sort_expr LIKE '%"key":"mc.cost_per_task"%';
