# ADR-001: Default Virtual Models und Sortierverhalten

## Status

Akzeptiert

## Kontext

llm-router braucht sinnvolle Default-Virtual-Modelle für typische Nutzungsfälle (Dokumentation, Code-Analyse, Planung, etc.). Zudem musste das Sortierverhalten für Modelle ohne Sortier-Key und numerische Werte korrigiert werden.

## Entscheidungen

### 1. Default Virtual Models

7 virtuelle Modelle mit aufgabenorientierten Filtern:

| Model | Filter | Zweck |
|-------|--------|-------|
| `docs` | intelligence ≥ 80, hallucination ≤ 20 | Dokumentation lesen/schreiben |
| `exploration` | intelligence ≥ 75, coding ≥ 70 | Code-Exploration |
| `planning` | intelligence ≥ 85 | Architektur/Planung |
| `refine` | coding ≥ 75, hallucination ≤ 25 | Code-Verfeinerung |
| `troubleshooting` | coding ≥ 70, intelligence ≥ 70 | Debugging |
| `brainstorming` | intelligence ≥ 40 | Kreatives Brainstorming (auch DeepSeek v4) |
| `chat` | intelligence ≥ 50, cost_type = free | Allgemeiner Chat |

Anlage per API (`POST /api/v1/virtual-models`), nicht per Seed-Command.

### 2. Default Sortierung: cost_per_task asc

Alle virtuellen Modelle sortieren primär nach `cost_per_task` aufsteigend (günstigste zuerst). Sekundäre Sortierung nach Use-Case (intelligence oder coding desc).

Begründung: Kosten sind der wichtigste Faktor für die Model-Auswahl bei Failover.

### 3. Modelle ohne Sortier-Key ganz unten

Modelle, die den primären Sortier-Key nicht haben (z.B. kein `cost_per_task`), werden ans Ende der Liste sortiert statt vorangestellt.

Vorher: Leerer String (`""`) wurde lexikographisch vor "0.02" sortiert → falsche Reihenfolge.
Nachher: Modelle ohne Key werden als letztes platziert.

Implementierung in:
- `internal/service/virtual_model_service.go` → `compareModels()`
- `internal/tui/vmview.go` → `sortResolved()`

### 4. Numerische Sortierung

Sortierung von Werten mit `direction: "asc"/"desc"` erfolgt numerisch, nicht lexikographisch.

Vorher: `"1.14" > "0.37"` (String-Vergleich) → falsche Reihenfolge.
Nachher: `1.14 > 0.37` (Numerisch) → korrekte Reihenfolge.

### 5. Reasoning-Effort-spezifische Auflösung

Jede Kombination aus Model + Reasoning-Effort wird einzeln in der resolved-Liste angezeigt. Modelle mit `high` und `max` Effort erscheinen jeweils eigene Einträge mit den jeweils passenden Tags.

API-Response enthält `reasoning_effort` Feld. TUI zeigt Effort in eckigen Klammern: `deepseek-v4-pro [max]`.

## Konsequenzen

- Virtuelle Modelle sind über API konfigurierbar, keine hardcoded Werte im Binary
- Sortierung funktioniert korrekt für numerische Kostenwerte
- Modelle ohne Kosteninformation werden fair behandelt (unten statt vorne)
- Reasoning-Effort wird bei der Modell-Auswahl berücksichtigt
