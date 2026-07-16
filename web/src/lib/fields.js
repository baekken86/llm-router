export function getFieldGroups(fields) {
  if (!fields) return [];
  const groups = { numeric: [], string: [], boolean: [] };
  for (const [key, def] of Object.entries(fields)) {
    const item = { key, ...def };
    switch (def.type) {
      case 'number': groups.numeric.push(item); break;
      case 'string': groups.string.push(item); break;
      case 'boolean': groups.boolean.push(item); break;
    }
  }
  return [
    { label: 'Numeric', fields: groups.numeric },
    { label: 'String', fields: groups.string },
    { label: 'Boolean', fields: groups.boolean }
  ].filter(g => g.fields.length > 0);
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
