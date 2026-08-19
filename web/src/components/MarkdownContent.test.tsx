import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { MarkdownContent } from './MarkdownContent'

describe('MarkdownContent', () => {
  it('折叠长代码块并允许展开和收起', async () => {
    const user = userEvent.setup()
    const code = Array.from({ length: 10 }, (_, index) => `const line${index + 1} = ${index + 1}`).join('\n')
    render(<MarkdownContent content={`说明\n\n\`\`\`ts\n${code}\n\`\`\``} />)

    expect(screen.getByText('const line1 = 1', { exact: false })).toBeInTheDocument()
    expect(screen.queryByText('const line10 = 10', { exact: false })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '展开代码，共 10 行' }))
    expect(screen.getByText('const line10 = 10', { exact: false })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '收起代码' }))
    expect(screen.queryByText('const line10 = 10', { exact: false })).not.toBeInTheDocument()
  })

  it('将 Artifact 图片显示为紧凑入口并支持全屏预览', async () => {
    const user = userEvent.setup()
    render(<MarkdownContent content="这里是截图：![登录错误](artifact://image-1)" />)

    const trigger = screen.getByRole('button', { name: '查看图片：登录错误' })
    expect(trigger).toHaveTextContent('[图片]')
    expect(screen.getByAltText('登录错误悬停预览')).toHaveAttribute(
      'src',
      '/api/artifacts/image-1/content?variant=optimized',
    )

    await user.click(trigger)
    expect(screen.getByRole('dialog', { name: '图片预览' })).toBeInTheDocument()
    expect(screen.getByAltText('登录错误')).toHaveAttribute(
      'src',
      '/api/artifacts/image-1/content?variant=original',
    )
    await user.click(screen.getByRole('button', { name: '关闭图片预览' }))
    expect(screen.queryByRole('dialog', { name: '图片预览' })).not.toBeInTheDocument()
  })

  it('不执行 Markdown 中的原始 HTML', () => {
    render(<MarkdownContent content={'<img src=x onerror="alert(1)">'} />)

    expect(screen.queryByRole('img')).not.toBeInTheDocument()
    expect(screen.getByText('<img src=x onerror="alert(1)">')).toBeInTheDocument()
  })
})
