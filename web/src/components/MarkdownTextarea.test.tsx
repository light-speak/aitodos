import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { UploadedImage } from '../types'
import { MarkdownTextarea } from './MarkdownTextarea'

function ControlledEditor({
  initialValue = '',
  onUploadImage,
  size,
}: {
  initialValue?: string
  onUploadImage?: (file: File) => Promise<UploadedImage>
  size?: 'large' | 'compact'
}) {
  const [value, setValue] = useState(initialValue)
  return <MarkdownTextarea aria-label="内容" value={value} onChange={setValue} onUploadImage={onUploadImage} size={size} />
}

describe('MarkdownTextarea', () => {
  it('提供大尺寸输入画布而不是独立边框 textarea', () => {
    render(<ControlledEditor size="large" />)

    const textarea = screen.getByRole('textbox', { name: '内容' })
    const composer = textarea.closest('[data-slot="markdown-composer"]')
    expect(composer).toHaveAttribute('data-size', 'large')
    expect(textarea).toHaveClass('resize-none', 'border-0')
  })

  it('把从代码区域复制的内容自动包成 Markdown 代码块', () => {
    render(<ControlledEditor />)
    const textarea = screen.getByRole('textbox', { name: '内容' })
    if (!(textarea instanceof HTMLTextAreaElement)) throw new Error('expected textarea')

    fireEvent.paste(textarea, {
      clipboardData: {
        files: [],
        getData: (type: string) => type === 'text/html'
          ? '<pre><code>const answer = 42\nconsole.log(answer)</code></pre>'
          : 'const answer = 42\nconsole.log(answer)',
      },
    })

    expect(textarea).toHaveValue('```\nconst answer = 42\nconsole.log(answer)\n```')
  })

  it('在说明文字后粘贴代码时自动补充分隔空行', () => {
    render(<ControlledEditor initialValue="需求说明" />)
    const textarea = screen.getByRole('textbox', { name: '内容' })
    if (!(textarea instanceof HTMLTextAreaElement)) throw new Error('expected textarea')
    textarea.setSelectionRange(textarea.value.length, textarea.value.length)

    fireEvent.paste(textarea, {
      clipboardData: {
        files: [],
        getData: (type: string) => type === 'text/html'
          ? '<pre><code>const answer = 42</code></pre>'
          : 'const answer = 42',
      },
    })

    expect(textarea).toHaveValue('需求说明\n\n```\nconst answer = 42\n```')
  })

  it('粘贴图片后上传并插入 Artifact Markdown 引用', async () => {
    const uploaded: UploadedImage = {
      id: 'image-1',
      markdown: '![图片](artifact://image-1)',
      original_url: '/api/artifacts/image-1/content?variant=original',
      optimized_url: '/api/artifacts/image-1/content?variant=optimized',
    }
    const onUploadImage = vi.fn<(file: File) => Promise<UploadedImage>>().mockResolvedValue(uploaded)
    render(<ControlledEditor onUploadImage={onUploadImage} />)
    const textarea = screen.getByRole('textbox', { name: '内容' })
    const image = new File(['image'], 'screen.png', { type: 'image/png' })

    fireEvent.paste(textarea, {
      clipboardData: {
        files: [image],
        getData: () => '',
      },
    })

    await waitFor(() => expect(onUploadImage).toHaveBeenCalledWith(image))
    await waitFor(() => expect(textarea).toHaveValue('![图片](artifact://image-1)'))
  })

  it('在代码块后粘贴图片时自动补充分隔空行', async () => {
    const uploaded: UploadedImage = {
      id: 'image-1',
      markdown: '![图片](artifact://image-1)',
      original_url: '/api/artifacts/image-1/content?variant=original',
      optimized_url: '/api/artifacts/image-1/content?variant=optimized',
    }
    const onUploadImage = vi.fn<(file: File) => Promise<UploadedImage>>().mockResolvedValue(uploaded)
    render(<ControlledEditor initialValue={'```\nconst answer = 42\n```'} onUploadImage={onUploadImage} />)
    const textarea = screen.getByRole('textbox', { name: '内容' })
    if (!(textarea instanceof HTMLTextAreaElement)) throw new Error('expected textarea')
    const image = new File(['image'], 'screen.png', { type: 'image/png' })

    textarea.setSelectionRange(textarea.value.length, textarea.value.length)
    fireEvent.paste(textarea, { clipboardData: { files: [image], getData: () => '' } })

    await waitFor(() => expect(textarea).toHaveValue('```\nconst answer = 42\n```\n\n![图片](artifact://image-1)'))
  })

  it('不会把普通多段文字误判为代码', () => {
    render(<ControlledEditor />)
    const textarea = screen.getByRole('textbox', { name: '内容' })

    fireEvent.paste(textarea, {
      clipboardData: {
        files: [],
        getData: (type: string) => type === 'text/plain' ? '第一段需求\n第二段补充说明' : '',
      },
    })

    expect(textarea).toHaveValue('第一段需求\n第二段补充说明')
  })
})
