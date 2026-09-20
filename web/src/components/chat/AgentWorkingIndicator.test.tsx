import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { AgentWorkingIndicator } from './AgentWorkingIndicator'

describe('AgentWorkingIndicator', () => {
  it('leaves route badges to the composer when requested', () => {
    const { container } = render(
      <AgentWorkingIndicator hideRouteInfo modelName="deepseek-v4-flash-202605" taskLevel="general" />,
    )
    expect(screen.queryByText('general')).not.toBeInTheDocument()
    expect(screen.queryByText('deepseek-v4-flash-202605')).not.toBeInTheDocument()
    expect(container.querySelector('.animate-led-breathing')).toBeInTheDocument()
  })

  it('shows the routed task type beside the model ID', () => {
    render(
      <AgentWorkingIndicator
        modelName="deepseek-v4-flash-202605"
        taskLevel="engineering"
      />,
    )

    expect(screen.getByText('engineering')).toBeInTheDocument()
    expect(screen.getByText('deepseek-v4-flash-202605')).toBeInTheDocument()
  })
})
