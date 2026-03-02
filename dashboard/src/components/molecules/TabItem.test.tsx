import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import TabItem from './TabItem'
import type { InstanceTab } from '../../generated/types'

const mockTab: InstanceTab = {
  id: 'tab_123',
  instanceId: 'inst_456',
  url: 'https://example.com/page',
  title: 'Example Page',
}

describe('TabItem', () => {
  it('renders tab title and url', () => {
    render(<TabItem tab={mockTab} />)
    
    expect(screen.getByText('Example Page')).toBeInTheDocument()
    expect(screen.getByText('https://example.com/page')).toBeInTheDocument()
  })

  it('shows Untitled for tabs without title', () => {
    const noTitleTab = { ...mockTab, title: '' }
    render(<TabItem tab={noTitleTab} />)
    
    expect(screen.getByText('Untitled')).toBeInTheDocument()
  })

  it('renders compact variant', () => {
    render(<TabItem tab={mockTab} compact />)
    
    expect(screen.getByText('Example Page')).toBeInTheDocument()
    // Compact uses border-b on the outer div
    const titleEl = screen.getByText('Example Page')
    const container = titleEl.parentElement
    expect(container).toHaveClass('border-b')
  })

  it('renders card variant by default', () => {
    render(<TabItem tab={mockTab} />)
    
    // Default uses Card component which has rounded class
    const container = screen.getByText('Example Page').closest('div')?.parentElement
    expect(container).toHaveClass('rounded-lg')
  })
})
