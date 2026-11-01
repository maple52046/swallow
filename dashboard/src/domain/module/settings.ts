import type { ModuleSetting, ModuleSpecSchema } from './types'

/**
 * Form text of every setting of `schema` from a recorded spec: a list is one entry per line, a
 * number or boolean its JSON text, and an unset setting an empty string.
 */
export function moduleSettingsFormValues(
  schema: ModuleSpecSchema,
  spec?: Record<string, unknown> | null,
): Record<string, string> {
  const values: Record<string, string> = {}
  for (const name of Object.keys(schema.properties)) {
    const value = spec?.[name]
    if (typeof value === 'string') values[name] = value
    else if (Array.isArray(value)) values[name] = value.filter((item): item is string => typeof item === 'string').join('\n')
    else if (typeof value === 'number' || typeof value === 'boolean') values[name] = String(value)
  }
  return values
}

/** Splits a list setting's text: commas, spaces, and line breaks all separate entries. */
function listEntries(text: string): string[] {
  return text.split(/[\s,]+/).map((item) => item.trim()).filter(Boolean)
}

/**
 * The spec a form's text means. A blank setting is omitted, a list split into entries, an integer
 * and a boolean parsed; text that does not parse is sent as typed, so the API's error names it.
 */
export function moduleSettingsSpec(schema: ModuleSpecSchema, values: Readonly<Record<string, string>>): Record<string, unknown> {
  const spec: Record<string, unknown> = {}
  for (const [name, setting] of Object.entries(schema.properties)) {
    const text = values[name]?.trim() ?? ''
    if (!text) continue
    if (setting.type === 'array') {
      const items = listEntries(text)
      if (items.length > 0) spec[name] = items
    } else if (setting.type === 'integer') {
      spec[name] = /^-?\d+$/.test(text) ? Number(text) : text
    } else if (setting.type === 'boolean') {
      spec[name] = text === 'true' ? true : text === 'false' ? false : text
    } else {
      spec[name] = text
    }
  }
  return spec
}

/**
 * Why one setting's text is not acceptable, or null. It mirrors the API's schema check for
 * immediate feedback; the API decides. A blank setting is a problem only when it is required.
 */
export function moduleSettingProblem(schema: ModuleSpecSchema, name: string, text: string): string | null {
  const setting = schema.properties[name]
  if (!setting) return null
  const trimmed = text.trim()
  if (!trimmed) return schema.required?.includes(name) ? `${setting.title} is required.` : null
  switch (setting.type) {
    case 'string':
      if (setting.enum && !setting.enum.includes(trimmed)) return `Choose one of ${setting.enum.join(', ')}.`
      if (setting.pattern && !new RegExp(setting.pattern).test(trimmed)) return `${setting.title} does not have the expected format.`
      return null
    case 'integer': {
      if (!/^-?\d+$/.test(trimmed)) return `${setting.title} must be a whole number.`
      const number = Number(trimmed)
      if (setting.minimum !== undefined && number < setting.minimum) return `${setting.title} must be at least ${setting.minimum}.`
      if (setting.maximum !== undefined && number > setting.maximum) return `${setting.title} must be at most ${setting.maximum}.`
      return null
    }
    case 'boolean':
      return trimmed === 'true' || trimmed === 'false' ? null : `${setting.title} must be on or off.`
    case 'array':
      return null
  }
}

/** Whether every setting's text is acceptable. */
export function moduleSettingsValid(schema: ModuleSpecSchema, values: Readonly<Record<string, string>>): boolean {
  return Object.keys(schema.properties).every((name) => moduleSettingProblem(schema, name, values[name] ?? '') === null)
}

/** Display text of a recorded setting value, or null when it is unset. */
export function moduleSettingDisplayValue(setting: ModuleSetting, value: unknown): string | null {
  if (value === null || value === undefined) return null
  if (Array.isArray(value)) return value.length > 0 ? value.map(String).join(', ') : null
  if (typeof value === 'boolean') return value ? 'On' : 'Off'
  const text = String(value).trim()
  if (!text) return null
  return setting.type === 'string' || setting.type === 'integer' ? text : JSON.stringify(value)
}
