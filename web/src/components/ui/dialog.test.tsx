import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Dialog, DialogTrigger, DialogContent, DialogTitle } from './dialog'
import { Button } from './button'

describe('Dialog', () => {
  it('opens when trigger is clicked and shows content', async () => {
    const user = userEvent.setup()
    render(
      <Dialog>
        <DialogTrigger render={<Button>Open</Button>} />
        <DialogContent>
          <DialogTitle>My Dialog</DialogTitle>
          <p>Content here</p>
        </DialogContent>
      </Dialog>
    )
    expect(screen.queryByText('My Dialog')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Open' }))
    expect(screen.getByText('My Dialog')).toBeInTheDocument()
    expect(screen.getByText('Content here')).toBeInTheDocument()
  })
})
