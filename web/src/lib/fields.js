export function getFieldGroups(fields) {
  if (!fields) return [];
  const namespaces = { 'Model Tags (mc.*)': [], 'Provider (p.*)': [], 'Global Model (m.*)': [] };
  for (const [key, def] of Object.entries(fields)) {
    const item = { key, ...def };
    if (key.startsWith('p.')) {
      namespaces['Provider (p.*)'].push(item);
    } else if (key.startsWith('m.')) {
      namespaces['Global Model (m.*)'].push(item);
    } else {
      namespaces['Model Tags (mc.*)'].push(item);
    }
  }
  return Object.entries(namespaces)
    .map(([label, fields]) => ({ label, fields }))
    .filter(g => g.fields.length > 0);
}

export function getFieldType(fields, key) {
  if (!fields || !fields[key]) return 'string';
  return fields[key].type;
}

export function getFieldValues(fields, key) {
  if (!fields || !fields[key]) return null;
  return fields[key].values || null;
}

export function getFieldMinMax(fields, key) {
  if (!fields || !fields[key]) return null;
  const def = fields[key];
  if (def.min !== undefined || def.max !== undefined) {
    return { min: def.min ?? 0, max: def.max ?? 100 };
  }
  return null;
}
