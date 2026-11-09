import { useState } from 'react'
import { IconButton, Input, InputGroup, type InputProps } from '@chakra-ui/react'
import { Eye, EyeOff } from 'lucide-react'
import { Tooltip } from './tooltip'

/** Every `Input` prop except `type`, which the visibility toggle owns. */
type PasswordInputProps = Omit<InputProps, 'type'>

/**
 * Shared password field with a show/hide toggle, so a user can check what they typed before it
 * is sent (a BMC password, for example, is write-only: it is never shown back afterwards).
 *
 * The value starts masked and is revealed only while the user keeps the toggle on; the toggle is a
 * labelled `type="button"` (never a form submit) that names the action it performs ("Show
 * password" / "Hide password"), and it is disabled with the field. While revealed, spell checking,
 * auto-correction, and capitalization are off so the browser neither alters the secret nor sends it
 * to a spelling service. The field keeps the caller's `autoComplete` (use `new-password` for a
 * secret that is not the user's own login). Inside a Chakra `Field`, the input keeps the field's
 * label and helper text.
 */
export function PasswordInput({ disabled, ...rest }: PasswordInputProps) {
  const [visible, setVisible] = useState(false)
  const label = visible ? 'Hide password' : 'Show password'
  return (
    <InputGroup
      endElement={
        <Tooltip content={label}>
          <IconButton
            type="button"
            size="xs"
            variant="ghost"
            color="fg.muted"
            me="-2"
            aria-label={label}
            disabled={disabled}
            onClick={() => setVisible((current) => !current)}
          >
            {visible ? <EyeOff size={16} /> : <Eye size={16} />}
          </IconButton>
        </Tooltip>
      }
    >
      <Input
        {...rest}
        type={visible ? 'text' : 'password'}
        disabled={disabled}
        spellCheck={false}
        autoCorrect="off"
        autoCapitalize="off"
      />
    </InputGroup>
  )
}
