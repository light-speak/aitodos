import { useState } from 'react'
import type { ReactNode } from 'react'
import { ChevronDownIcon, ChevronUpIcon, ImageIcon, XIcon } from 'lucide-react'

import { Button } from './ui/button'
import { Dialog, DialogContent, DialogTitle } from './ui/dialog'

interface MarkdownContentProps {
  content: string
  emptyText?: string
}

type Block =
  | { kind: 'code'; language: string; content: string }
  | { kind: 'heading'; level: number; content: string }
  | { kind: 'quote'; content: string }
  | { kind: 'list'; ordered: boolean; items: string[] }
  | { kind: 'paragraph'; content: string }

interface PreviewImage {
  id: string
  label: string
}

export function MarkdownContent({ content, emptyText = '未填写' }: MarkdownContentProps) {
  const [preview, setPreview] = useState<PreviewImage | null>(null)
  if (!content.trim()) {
    return <p className="text-sm text-muted-foreground">{emptyText}</p>
  }
  return (
    <>
      <div className="grid gap-3 break-words text-sm leading-6">
        {parseBlocks(content).map((block, index) => (
          <MarkdownBlock block={block} key={`${block.kind}-${index}`} onPreview={setPreview} />
        ))}
      </div>
      {preview ? <ImagePreview image={preview} onClose={() => setPreview(null)} /> : null}
    </>
  )
}

function MarkdownBlock({ block, onPreview }: { block: Block; onPreview: (image: PreviewImage) => void }) {
  switch (block.kind) {
  case 'code':
    return <CollapsibleCode language={block.language} content={block.content} />
  case 'heading': {
    const className = block.level === 1 ? 'text-lg font-semibold' : 'text-base font-semibold'
    return <p className={className}>{renderInline(block.content, onPreview)}</p>
  }
  case 'quote':
    return <blockquote className="border-l-2 pl-3 text-muted-foreground">{renderInline(block.content, onPreview)}</blockquote>
  case 'list': {
    const List = block.ordered ? 'ol' : 'ul'
    return (
      <List className={`grid gap-1 pl-5 ${block.ordered ? 'list-decimal' : 'list-disc'}`}>
        {block.items.map((item, index) => <li key={index}>{renderInline(item, onPreview)}</li>)}
      </List>
    )
  }
  case 'paragraph':
    return <p className="whitespace-pre-wrap">{renderInline(block.content, onPreview)}</p>
  }
}

function CollapsibleCode({ language, content }: { language: string; content: string }) {
  const [expanded, setExpanded] = useState(false)
  const lines = content.split('\n')
  const collapsible = lines.length > 5 || content.length > 600
  const visible = collapsible && !expanded ? lines.slice(0, 4).join('\n') : content
  return (
    <div className="overflow-hidden rounded-lg border bg-zinc-950 text-zinc-100">
      <div className="flex min-h-8 items-center justify-between border-b border-white/10 px-3 text-[11px] text-zinc-400">
        <span>{language || '代码'} · {lines.length} 行</span>
      </div>
      <pre className="overflow-x-auto p-3 text-xs leading-5"><code>{visible}</code></pre>
      {collapsible ? (
        <button
          className="flex w-full items-center justify-center gap-1 border-t border-white/10 py-1.5 text-xs text-zinc-300 hover:bg-white/5"
          type="button"
          aria-label={expanded ? '收起代码' : `展开代码，共 ${lines.length} 行`}
          onClick={() => setExpanded((current) => !current)}
        >
          {expanded ? <ChevronUpIcon className="size-3.5" /> : <ChevronDownIcon className="size-3.5" />}
          {expanded ? '收起' : `展开其余 ${Math.max(0, lines.length - 4)} 行`}
        </button>
      ) : null}
    </div>
  )
}

function ArtifactImage({ id, label, onPreview }: PreviewImage & { onPreview: (image: PreviewImage) => void }) {
  return (
    <span className="group relative inline-flex align-baseline">
      <button
        className="inline-flex items-center gap-1 rounded-md border bg-muted/60 px-1.5 py-0.5 text-xs font-medium hover:bg-muted"
        type="button"
        aria-label={`查看图片：${label}`}
        onClick={() => onPreview({ id, label })}
      >
        <ImageIcon className="size-3.5" />[图片]
      </button>
      <span className="pointer-events-none absolute bottom-full left-0 z-30 mb-2 hidden w-80 rounded-lg border bg-popover p-2 shadow-xl group-hover:block" aria-hidden="true">
        <img
          className="max-h-60 w-full rounded object-contain"
          src={imageURL(id, 'optimized')}
          alt={`${label}悬停预览`}
        />
      </span>
    </span>
  )
}

function ImagePreview({ image, onClose }: { image: PreviewImage; onClose: () => void }) {
  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="h-[calc(100svh-2rem)] max-w-[calc(100vw-2rem)] grid-rows-[auto_minmax(0,1fr)] bg-zinc-950 p-3" showCloseButton={false}>
        <div className="flex items-center justify-between gap-4 text-white">
          <DialogTitle>图片预览</DialogTitle>
          <Button className="text-white hover:bg-white/10 hover:text-white" variant="ghost" size="icon-sm" type="button" aria-label="关闭图片预览" onClick={onClose}>
            <XIcon />
          </Button>
        </div>
        <div className="flex min-h-0 items-center justify-center overflow-auto">
          <img className="max-h-full max-w-full object-contain" src={imageURL(image.id, 'original')} alt={image.label} />
        </div>
      </DialogContent>
    </Dialog>
  )
}

function parseBlocks(content: string): Block[] {
  const lines = content.replace(/\r\n/g, '\n').split('\n')
  const blocks: Block[] = []
  for (let index = 0; index < lines.length;) {
    const line = lineAt(lines, index)
    if (!line.trim()) {
      index += 1
      continue
    }
    const fence = line.match(/^\s*```([^`]*)$/)
    if (fence) {
      const code: string[] = []
      index += 1
      while (index < lines.length && !/^\s*```\s*$/.test(lineAt(lines, index))) code.push(lineAt(lines, index++))
      if (index < lines.length) index += 1
      blocks.push({ kind: 'code', language: (fence[1] ?? '').trim(), content: code.join('\n') })
      continue
    }
    const heading = line.match(/^(#{1,3})\s+(.+)$/)
    if (heading) {
      blocks.push({ kind: 'heading', level: (heading[1] ?? '').length, content: heading[2] ?? '' })
      index += 1
      continue
    }
    if (/^\s*>\s?/.test(line)) {
      const quoted: string[] = []
      while (index < lines.length && /^\s*>\s?/.test(lineAt(lines, index))) quoted.push(lineAt(lines, index++).replace(/^\s*>\s?/, ''))
      blocks.push({ kind: 'quote', content: quoted.join('\n') })
      continue
    }
    const listMatch = line.match(/^\s*(?:([-*])|(\d+)\.)\s+(.+)$/)
    if (listMatch) {
      const ordered = Boolean(listMatch[2])
      const items: string[] = []
      while (index < lines.length) {
        const item = lineAt(lines, index).match(/^\s*(?:([-*])|(\d+)\.)\s+(.+)$/)
        if (!item || Boolean(item[2]) !== ordered) break
        items.push(item[3] ?? '')
        index += 1
      }
      blocks.push({ kind: 'list', ordered, items })
      continue
    }
    const paragraph: string[] = []
    while (index < lines.length && lineAt(lines, index).trim() && !startsBlock(lineAt(lines, index), paragraph.length > 0)) {
      paragraph.push(lineAt(lines, index++))
    }
    if (paragraph.length === 0) paragraph.push(lineAt(lines, index++))
    blocks.push({ kind: 'paragraph', content: paragraph.join('\n') })
  }
  return blocks
}

function lineAt(lines: string[], index: number): string {
  return lines[index] ?? ''
}

function startsBlock(line: string, allowParagraphStart: boolean): boolean {
  if (!allowParagraphStart) return false
  return /^\s*```|^(?:#{1,3})\s+|^\s*>\s?|^\s*(?:[-*]|\d+\.)\s+/.test(line)
}

function renderInline(content: string, onPreview: (image: PreviewImage) => void): ReactNode[] {
  const pattern = /!\[([^\]]*)\]\(artifact:\/\/([A-Za-z0-9-]+)\)|`([^`\n]+)`|\[([^\]]+)\]\(([^)]+)\)|\*\*([^*]+)\*\*/g
  const nodes: ReactNode[] = []
  let cursor = 0
  for (const match of content.matchAll(pattern)) {
    const index = match.index
    if (index > cursor) nodes.push(content.slice(cursor, index))
    if (match[2]) {
      nodes.push(<ArtifactImage id={match[2]} label={match[1] || '图片'} onPreview={onPreview} key={index} />)
    } else if (match[3]) {
      nodes.push(<code className="rounded bg-muted px-1 py-0.5 font-mono text-xs" key={index}>{match[3]}</code>)
    } else if (match[4]) {
      const href = match[5] ?? ''
      nodes.push(safeLink(href)
        ? <a className="underline underline-offset-3 hover:text-foreground" href={href} target="_blank" rel="noreferrer" key={index}>{match[4]}</a>
        : match[0])
    } else {
      nodes.push(<strong key={index}>{match[6]}</strong>)
    }
    cursor = index + match[0].length
  }
  if (cursor < content.length) nodes.push(content.slice(cursor))
  return nodes
}

function safeLink(value: string): boolean {
  return /^(?:https?:\/\/|mailto:)/i.test(value)
}

function imageURL(id: string, variant: 'original' | 'optimized'): string {
  return `/api/artifacts/${encodeURIComponent(id)}/content?variant=${variant}`
}
