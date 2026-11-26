import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { AgentWorkingIndicator } from './AgentWorkingIndicator'

describe('AgentWorkingIndicator', () => {
  it('keeps the task type in the stream while the composer owns the engine badges', () => {
    const { container } = render(
      <AgentWorkingIndicator
        hideModelInfo
        providerName="deepseek"
        modelName="deepseek/deepseek-v4-flash-202605"
        taskLevel="general"
      />,
    )
    expect(screen.getByText('general')).toBeInTheDocument()
    expect(screen.queryByText('deepseek')).not.toBeInTheDocument()
    expect(screen.queryByText('deepseek/deepseek-v4-flash-202605')).not.toBeInTheDocument()
    expect(container.querySelector('.animate-led-breathing')).toBeInTheDocument()
  })

  it('shows separate task type, provider, and model badges', () => {
    render(
      <AgentWorkingIndicator
        providerName="deepseek"
        modelName="deepseek/deepseek-v4-flash-202605"
        taskLevel="engineering"
      />,
    )

    expect(screen.getByText('engineering')).toBeInTheDocument()
    expect(screen.getByText('deepseek')).toBeInTheDocument()
    expect(screen.getByText('deepseek/deepseek-v4-flash-202605')).toBeInTheDocument()
  })

  it('keeps three placeholders while route metadata is pending', () => {
    const { container } = render(
      <AgentWorkingIndicator providerName="" modelName="" taskLevel="" />,
    )

    expect(container.querySelectorAll('.animate-pulse')).toHaveLength(3)
  })

  it('keeps a single task type placeholder when the composer owns the engine badges', () => {
    const { container } = render(
      <AgentWorkingIndicator hideModelInfo providerName="" modelName="" taskLevel="" />,
    )

    expect(container.querySelectorAll('.animate-pulse')).toHaveLength(1)
  })

  it('hides route badges when route metadata is undefined', () => {
    const { container } = render(<AgentWorkingIndicator />)

    expect(container.querySelector('.animate-pulse')).not.toBeInTheDocument()
    expect(container.querySelector('.animate-led-breathing')).toBeInTheDocument()
  })
})
