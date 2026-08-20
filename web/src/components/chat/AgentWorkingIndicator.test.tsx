import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { AgentWorkingIndicator } from './AgentWorkingIndicator'

describe('AgentWorkingIndicator', () => {
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
