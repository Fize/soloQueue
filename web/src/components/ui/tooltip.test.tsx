import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from './tooltip'

describe('Tooltip', () => {
  it('shows content on hover', async () => {
    const user = userEvent.setup()
    render(
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger>Hover me</TooltipTrigger>
          <TooltipContent>Tooltip text</TooltipContent>
        </Tooltip>
      </TooltipProvider>
    )
    expect(screen.queryByText('Tooltip text')).not.toBeInTheDocument()
    await user.hover(screen.getByText('Hover me'))
    expect(await screen.findByText('Tooltip text')).toBeInTheDocument()
  })

  it('composes with an existing interactive trigger without nesting buttons', () => {
    render(
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger render={<button aria-label="Refresh cron tasks">Refresh</button>} />
          <TooltipContent>Refresh tasks</TooltipContent>
        </Tooltip>
      </TooltipProvider>
    )

    const trigger = screen.getByRole('button', { name: 'Refresh cron tasks' })
    expect(trigger).toBeInTheDocument()
    expect(trigger.querySelector('button')).toBeNull()
  })
})
