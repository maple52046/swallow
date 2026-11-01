/** What an Extension Module adds to swallow (glossary Extension Module). */
export type ModuleKind = 'accelerator' | 'platform' | 'software'

/** The value type of one module setting; an `array` is a list of strings. */
export type ModuleSettingType = 'string' | 'integer' | 'boolean' | 'array'

/**
 * One setting of a module's spec schema (contract `modules.md`). `examples` are sample values of
 * the setting's type; `enum` and `pattern` apply to a string, `minimum` and `maximum` to an integer.
 */
export interface ModuleSetting {
  type: ModuleSettingType
  title: string
  description?: string
  examples?: unknown[]
  enum?: string[]
  pattern?: string
  minimum?: number
  maximum?: number
  items?: { type: 'string' }
}

/**
 * The JSON Schema subset a module declares its spec with. `properties` keep the manifest's order,
 * which is the order a generic form shows them in.
 */
export interface ModuleSpecSchema {
  type: 'object'
  additionalProperties: false
  required?: string[]
  properties: Record<string, ModuleSetting>
}

/** An Extension Module compiled into the running build, with the spec schema it declares. */
export interface ExtensionModule {
  kind: ModuleKind
  id: string
  version: string
  spec: ModuleSpecSchema | null
}
