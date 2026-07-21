export function abbrevKey(key) {
  const map = {
    'mc.intelligence': 'int',
    'mc.coding': 'code',
    'mc.speed': 'spd',
    'mc.cost_per_task': '$/task',
    'mc.cost_per_1m_input': '$/1M',
    'mc.cost_per_1m_output': '$/1M.out',
    'mc.cost_per_1m_cache': '$/1M.cache',
    'mc.hallucination': 'hall',
    'mc.latency': 'lat',
    'mc.context_window': 'ctx',
    'mc.cost_type': 'cost_type',
    'mc.has_reasoning_effort': 'has_effort',
    'mc.reasoning': 'reason',
  };
  return map[key] || key;
}
