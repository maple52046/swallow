import { useState, type ReactNode } from 'react'
import { RadioCard } from '@chakra-ui/react'

/** One choice of a {@link ChoiceCardGroup}: the value it emits, its title, and what it means. */
export interface ChoiceCardOption<T extends string> {
  value: T
  label: string
  /** The consequence of the choice, readable before it is made. */
  description: ReactNode
}

interface ChoiceCardGroupProps<T extends string> {
  /** The radio group's accessible name. */
  label: string
  options: ReadonlyArray<ChoiceCardOption<T>>
  /** The selected value; the group is controlled and shows only this value as selected. */
  value: T
  /**
   * Called with the value the user picks. The caller must select it through `value`, even while it
   * still asks to confirm, because the click has already checked the native radio; it can select the
   * previous value again afterwards.
   */
  onChange: (value: T) => void
  disabled?: boolean
  /** How many cards share a row on a wide screen (two by default); a phone always stacks them. */
  columns?: 2 | 3
}

/**
 * A single choice among a few equal-width radio cards, each with a title and a description of what
 * it does: the deployment addressing mode (Deploy OS, Deployment Templates, the platform deploy
 * wizard) and where a Site's deployments fetch artifacts (Local repository page).
 *
 * It is a native radio group, so arrow keys move between the choices and each emits `onChange`. The
 * caller owns `value` and selects the picked value at once: a form keeps it, while a setting that
 * confirms first selects the stored value again when the change is cancelled. The focus ring shows
 * only after keyboard use, because a pointer click on a card should not leave an outline behind.
 */
export function ChoiceCardGroup<T extends string>({ label, options, value, onChange, disabled, columns = 2 }: ChoiceCardGroupProps<T>) {
  const [keyboardNavigation, setKeyboardNavigation] = useState(false)

  return (
    <RadioCard.Root
      aria-label={label}
      colorPalette="brand"
      value={value}
      disabled={disabled}
      className={columns === 3 ? 'sw-deployment-choice-grid sw-deployment-choice-grid--three' : 'sw-deployment-choice-grid'}
      data-focus-modality={keyboardNavigation ? 'keyboard' : 'pointer'}
      onKeyDownCapture={() => setKeyboardNavigation(true)}
      onPointerDownCapture={() => setKeyboardNavigation(false)}
      onValueChange={(details) => {
        // Only rendered values are emitted; the lookup also ignores the null of a cleared selection.
        const option = options.find((candidate) => candidate.value === details.value)
        if (option) onChange(option.value)
      }}
    >
      {options.map((option) => (
        <RadioCard.Item key={option.value} value={option.value}>
          <RadioCard.ItemHiddenInput />
          <RadioCard.ItemControl className="sw-deployment-choice-card">
            <RadioCard.ItemContent className="sw-deployment-choice-card__content">
              <RadioCard.ItemText className="sw-deployment-choice-card__title">{option.label}</RadioCard.ItemText>
              <RadioCard.ItemDescription className="sw-deployment-choice-card__description">{option.description}</RadioCard.ItemDescription>
            </RadioCard.ItemContent>
            <RadioCard.ItemIndicator />
          </RadioCard.ItemControl>
        </RadioCard.Item>
      ))}
    </RadioCard.Root>
  )
}
