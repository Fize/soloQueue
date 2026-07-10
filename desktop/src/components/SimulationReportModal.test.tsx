import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { SimulationReportModal } from './SimulationReportModal'

describe('SimulationReportModal', () => {
  it('renders report content when open', () => {
    render(
      <SimulationReportModal
        open={true}
        onOpenChange={() => {}}
        report="This is a test report."
        topic="Test Simulation"
      />
    )
    // The heading text is rendered inside the Dialog
    expect(screen.getByText('仿真最终分析报告 (全文阅读)')).toBeInTheDocument()
    expect(screen.getByText('Test Simulation')).toBeInTheDocument()
    // The report text (plain text, no markdown) should be visible
    expect(screen.getByText('This is a test report.')).toBeInTheDocument()
  })

  it('renders topic as subtitle when provided', () => {
    render(
      <SimulationReportModal
        open={true}
        onOpenChange={() => {}}
        report="content"
        topic="My Topic"
      />
    )
    expect(screen.getByText('My Topic')).toBeInTheDocument()
  })

  it('does not show topic element when topic is undefined', () => {
    render(
      <SimulationReportModal
        open={true}
        onOpenChange={() => {}}
        report="content"
        topic={undefined}
      />
    )
    expect(screen.queryByText('仿真最终分析报告 (全文阅读)')).toBeInTheDocument()
    // Topic span should not exist
    const title = screen.getByText('仿真最终分析报告 (全文阅读)')
    const parent = title.parentElement
    expect(parent?.querySelector('span.font-mono')).toBeNull()
  })
})
