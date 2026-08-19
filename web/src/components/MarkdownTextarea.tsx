import { useEffect, useRef, useState } from 'react'
import type { ClipboardEvent, ComponentProps } from 'react'

import { errorMessage, uploadImage } from '../api/client'
import type { UploadedImage } from '../types'
import { Textarea } from './ui/textarea'

type TextareaProps = Omit<ComponentProps<'textarea'>, 'onChange' | 'onPaste' | 'value'>

interface MarkdownTextareaProps extends TextareaProps {
  value: string
  onChange: (value: string) => void
  onUploadImage?: (file: File) => Promise<UploadedImage>
  size?: 'large' | 'compact'
}

export function MarkdownTextarea({
  value,
  onChange,
  onUploadImage = uploadImage,
  size = 'compact',
  className,
  ...props
}: MarkdownTextareaProps) {
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const valueRef = useRef(value)
  const uploadSequence = useRef(0)
  const [uploading, setUploading] = useState(0)
  const [uploadError, setUploadError] = useState('')

  useEffect(() => {
    valueRef.current = value
  }, [value])

  function updateValue(next: string) {
    valueRef.current = next
    onChange(next)
  }

  function insertAtSelection(text: string) {
    const textarea = textareaRef.current
    const start = textarea?.selectionStart ?? valueRef.current.length
    const end = textarea?.selectionEnd ?? start
    const next = valueRef.current.slice(0, start) + text + valueRef.current.slice(end)
    updateValue(next)
    requestAnimationFrame(() => {
      const cursor = start + text.length
      textareaRef.current?.setSelectionRange(cursor, cursor)
    })
  }

  async function insertImage(file: File) {
    const marker = `![图片上传中-${++uploadSequence.current}]()`
    insertAtSelection(spacedBlock(valueRef.current, textareaRef.current, marker))
    setUploading((current) => current + 1)
    setUploadError('')
    try {
      const uploaded = await onUploadImage(file)
      updateValue(valueRef.current.replace(marker, uploaded.markdown))
    } catch (error: unknown) {
      updateValue(valueRef.current.replace(marker, ''))
      setUploadError(errorMessage(error))
    } finally {
      setUploading((current) => current - 1)
    }
  }

  function handlePaste(event: ClipboardEvent<HTMLTextAreaElement>) {
    const images = Array.from(event.clipboardData.files).filter((file) => file.type.startsWith('image/'))
    if (images.length > 0) {
      event.preventDefault()
      for (const image of images) void insertImage(image)
      return
    }
    const text = event.clipboardData.getData('text/plain')
    if (!text) return
    event.preventDefault()
    const html = event.clipboardData.getData('text/html')
    const pasted = fenceCode(text)
    insertAtSelection(shouldWrapCode(text, html) ? spacedBlock(valueRef.current, textareaRef.current, pasted) : text)
  }

  return (
    <div
      className="overflow-hidden rounded-2xl border bg-card shadow-sm transition-[border-color,box-shadow] hover:border-foreground/20 focus-within:border-foreground/30 focus-within:ring-4 focus-within:ring-ring/10"
      data-slot="markdown-composer"
      data-size={size}
    >
      <Textarea
        {...props}
        ref={textareaRef}
        className={`${size === 'large' ? 'min-h-64 max-h-[55svh] px-5 py-4 text-base leading-7 md:text-base' : 'min-h-28 max-h-64 px-4 py-3.5 leading-6'} ${className ?? ''} resize-none rounded-none border-0 bg-transparent shadow-none ring-0 focus-visible:border-transparent focus-visible:ring-0 dark:bg-transparent`}
        value={value}
        onChange={(event) => updateValue(event.target.value)}
        onPaste={handlePaste}
      />
      <div className="flex min-h-9 flex-wrap items-center justify-between gap-x-3 border-t bg-muted/20 px-4 py-2 text-[11px] text-muted-foreground">
        <span>Markdown · 可直接粘贴代码和图片</span>
        {uploading > 0 ? <span role="status">正在处理图片…</span> : null}
        {uploadError ? <span className="text-destructive" role="alert">{uploadError}</span> : null}
      </div>
    </div>
  )
}

function spacedBlock(value: string, textarea: HTMLTextAreaElement | null, block: string): string {
  const start = textarea?.selectionStart ?? value.length
  const end = textarea?.selectionEnd ?? start
  const before = value.slice(0, start)
  const after = value.slice(end)
  const prefix = before.length === 0 || before.endsWith('\n\n') ? '' : before.endsWith('\n') ? '\n' : '\n\n'
  const suffix = after.length === 0 || after.startsWith('\n\n') ? '' : after.startsWith('\n') ? '\n' : '\n\n'
  return prefix + block + suffix
}

function shouldWrapCode(text: string, html: string): boolean {
  if (text.trimStart().startsWith('```')) return false
  if (/<(?:pre|code)(?:\s|>)/i.test(html)) return true
  const lines = text.split(/\r?\n/)
  if (lines.length < 2) return false
  const signals = lines.reduce((score, line) => {
    const trimmed = line.trim()
    if (/^(?:const|let|var|func|function|class|import|export|package|type|interface|SELECT|INSERT|UPDATE|curl|git)\b/.test(trimmed)) return score + 2
    if (/[{};]|=>|:=|\(|\)/.test(trimmed)) return score + 1
    if (/^(?:\s{2,}|\t)/.test(line)) return score + 1
    return score
  }, 0)
  return signals >= Math.max(2, Math.ceil(lines.length / 2))
}

function fenceCode(text: string): string {
  return `\`\`\`\n${text.replace(/\s+$/, '')}\n\`\`\``
}
