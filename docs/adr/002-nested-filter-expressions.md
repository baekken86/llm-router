# ADR-002: Nested Filter Expressions (AND/OR/NOT)

## Status

Entwurf

## Kontext

Das aktuelle Filtersystem unterstützt nur flache AND-Verknüpfungen:

```json
{ "and": [{ "key": "mc.intelligence", "op": "gte", "value": 80 }] }
```

Das reicht nicht für Szenarien wie:

- Claude Pro Abo: Models bei Anthropic sind teurer, sollen aber mit höherem Kostenlimit erlaubt sein
- Freie Models: Kostenlimit <= 0.5, aber bestimmte Provider (z.B. Anthropic) dürfen bis 1.5

Konkretes Beispiel:
> "Wenn Provider == Anthropic, dann cost_per_task <= 1.5, sonst cost_per_task <= 0.5"

Das erfordert OR-Logik mit verschachtelten AND-Bedingungen.

## Entscheidungen

### 1. Rekursiver Filterbaum

`FilterExpr` wird durch einen rekursiven `FilterNode` ersetzt:

```go
type FilterNode struct {
    // Leaf (einfache Bedingung)
    Key   string      `json:"key,omitempty"`
    Op    string      `json:"op,omitempty"`
    Value interface{} `json:"value,omitempty"`

    // Knoten (logische Verknüpfung)
    And []FilterNode `json:"and,omitempty"`
    Or  []FilterNode `json:"or,omitempty"`
    Not *FilterNode  `json:"not,omitempty"`
}
```

Ein Knoten ist entweder:
- **Leaf**: `key` + `op` + `value` gesetzt → einfache Bedingung
- **AND**: `and` gesetzt → alle Kinder müssen wahr sein
- **OR**: `ob` gesetzt → mindestens ein Kind muss wahr sein
- **NOT**: `not` gesetzt → Kind muss unwahr sein

### 2. JSON-Format

Beispiel für das obige Szenario:

```json
{
  "or": [
    {
      "and": [
        { "key": "p.name", "op": "eq", "value": "anthropic" },
        { "key": "m.cost_per_task", "op": "lte", "value": 1.5 }
      ]
    },
    {
      "key": "m.cost_per_task", "op": "lte", "value": 0.5
    }
  ]
}
```

### 3. Abwärtskompatibilität

Das alte Format `{ "and": [...] }` funktioniert weiterhin — ein `and`-Knoten mit Leaf-Kindern ist identisch zum bisherigen Verhalten.

Bestehende Virtual Models in der Datenbank müssen nicht migriert werden.

### 4. Evaluierung

`matchesFilter` wird rekursiv:

```
eval(node):
  if node.Key != ""  → evaluateCondition(actual, node.Op, node.Value)
  if node.And != nil → alle Kinder eval()
  if node.Or  != nil → mindestens ein Kind eval()
  if node.Not != nil → !eval(node.Not)
```

### 5. UI: Verschachtelte Gruppen

Der `ConditionBuilder` wird durch eine rekursive `FilterGroup`-Komponente ersetzt:

- Jede Gruppe hat ein AND/OR-Toggle und eine Liste von Elementen
- Elemente können einfache Conditions oder verschachtelte Sub-Gruppen sein
- Jede Condition hat ein NOT-Toggle
- Gruppen werden durch Einrückung visuell verschachtelt dargestellt

### 6. Validierung

`validateFilterExpr` prüft rekursiv:
- Leaf: `key`, `op`, `value` müssen gesetzt sein; `op` muss gültig sein
- AND/OR: `and`/`or` muss ein nicht-leeres Array sein
- NOT: `not` muss ein einzelner Knoten sein
- Kein Knoten darf gleichzeitig `key` und `and`/`or`/`not` haben (Mischtung)

## Betroffene Dateien

| Datei | Änderung |
|-------|----------|
| `internal/models/virtual_model.go` | `FilterExpr`/`FilterCondition` → `FilterNode` |
| `internal/service/virtual_model_service.go` | `matchesFilter` rekursiv, `validateFilterExpr` rekursiv |
| `web/src/components/ConditionBuilder.svelte` | Rekursive `FilterGroup`-Komponente |
| `web/src/components/VirtualModelForm.svelte` | Anpassung an neues Filter-Format |

## Konsequenzen

- Komplexe Routing-Logik möglich (OR, NOT, verschachtelte Bedingungen)
- Abwärtskompatibel: bestehende Filter funktionieren weiterhin
- Kein Schema-Migration nötig (JSON-RAW-Speicherung)
- UI wird komplexer, aber Einrückung hält es lesbar
