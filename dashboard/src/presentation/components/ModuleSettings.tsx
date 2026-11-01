import { Field, Input, Stack, Text, Textarea } from '@chakra-ui/react'
import { moduleSettingDisplayValue, moduleSettingProblem } from '@/domain/module/settings'
import type { ModuleSetting, ModuleSpecSchema } from '@/domain/module/types'
import { DescriptionList } from '@/presentation/components/ui/description-list'
import { Select } from '@/presentation/components/ui/select'

/** Placeholder text of a setting's first example; a list example is one entry per line. */
function examplePlaceholder(setting: ModuleSetting): string | undefined {
  const example = setting.examples?.[0]
  if (example === undefined || example === null) return undefined
  return Array.isArray(example) ? example.map(String).join('\n') : String(example)
}

interface ModuleSettingsFieldsProps {
  schema: ModuleSpecSchema
  /** The form text of a setting. */
  value: (name: string) => string
  onChange: (name: string, text: string) => void
  /** Keeps the ids of choice controls unique on the page. */
  idPrefix: string
  disabled?: boolean
}

/**
 * The generic settings form of a module without dedicated screens, generated from its spec schema
 * in the schema's order: a text field per string (a choice when it lists values), a numeric field
 * per integer, an On, Off, or Default choice per boolean, and one entry per line for a list. A
 * format problem shows once the field has text; a missing required setting shows as required.
 */
export function ModuleSettingsFields({ schema, value, onChange, idPrefix, disabled = false }: ModuleSettingsFieldsProps) {
  return (
    <Stack gap="4">
      {Object.entries(schema.properties).map(([name, setting]) => {
        const text = value(name)
        const problem = moduleSettingProblem(schema, name, text)
        const required = schema.required?.includes(name) ?? false
        const id = `${idPrefix}-${name}`
        const choices = setting.type === 'boolean'
          ? [{ value: 'true', label: 'On' }, { value: 'false', label: 'Off' }]
          : setting.type === 'string' && setting.enum
            ? setting.enum.map((choice) => ({ value: choice, label: choice }))
            : null
        return (
          <Field.Root key={name} required={required} invalid={problem !== null && text.trim() !== ''} disabled={disabled}>
            <Field.Label id={`${id}-label`}>
              {setting.title}
              {required && <Field.RequiredIndicator />}
            </Field.Label>
            {choices ? (
              <Select
                id={id}
                aria-label={setting.title}
                value={text}
                disabled={disabled}
                onChange={(next) => onChange(name, next)}
                options={[{ value: '', label: required ? 'Choose a value' : 'Default' }, ...choices]}
              />
            ) : setting.type === 'array' ? (
              <Textarea
                value={text}
                onChange={(event) => onChange(name, event.target.value)}
                placeholder={examplePlaceholder(setting)}
                rows={3}
              />
            ) : (
              <Input
                value={text}
                inputMode={setting.type === 'integer' ? 'numeric' : undefined}
                onChange={(event) => onChange(name, event.target.value)}
                placeholder={examplePlaceholder(setting)}
              />
            )}
            {(setting.description || setting.type === 'array') && (
              <Field.HelperText>
                {[setting.description, setting.type === 'array' ? 'One entry per line; commas also work.' : null].filter(Boolean).join(' ')}
              </Field.HelperText>
            )}
            {problem && <Field.ErrorText>{problem}</Field.ErrorText>}
          </Field.Root>
        )
      })}
    </Stack>
  )
}

/**
 * The generic details of a module's recorded settings: each set setting under its schema title, in
 * the schema's order. Settings left unset are omitted because the module's defaults apply to them.
 */
export function ModuleSettingsDetails({ schema, spec }: { schema: ModuleSpecSchema; spec: Record<string, unknown> | null | undefined }) {
  const items = Object.entries(schema.properties).flatMap(([name, setting]) => {
    const display = moduleSettingDisplayValue(setting, spec?.[name])
    return display === null ? [] : [{ label: setting.title, value: display }]
  })
  if (items.length === 0) return <Text color="fg.muted">No settings are recorded; the defaults apply.</Text>
  return <DescriptionList items={items} />
}
