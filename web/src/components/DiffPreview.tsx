import type { ReactNode } from 'react'
import { XIcon } from 'lucide-react'

import type { FileDiff } from '../types'
import { Button } from './ui/button'

type DiffLineKind = 'addition' | 'deletion' | 'hunk' | 'header' | 'context'

interface LanguageDefinition {
	label: string
	keywords: ReadonlySet<string>
	slashComments?: boolean
	hashComments?: boolean
	sqlComments?: boolean
}

const goDefinition = language('Go', 'break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var')
const typeScriptDefinition = language('TypeScript', 'as async await break case catch class const continue debugger default delete do else enum export extends false finally for from function get if implements import in infer instanceof interface keyof let module namespace never new null of private protected public readonly return satisfies set static string super switch symbol this throw true try type typeof undefined unknown var void while yield')
const javaScriptDefinition = language('JavaScript', 'async await break case catch class const continue debugger default delete do else export extends false finally for from function get if import in instanceof let new null of return set static super switch this throw true try typeof undefined var void while yield')
const jsonDefinition = language('JSON', 'false null true')
const pythonDefinition = { ...language('Python', 'and as assert async await break class continue def del elif else except false finally for from global if import in is lambda none nonlocal not or pass raise return true try while with yield'), hashComments: true }
const rustDefinition = language('Rust', 'as async await break const continue crate dyn else enum extern false fn for if impl in let loop match mod move mut pub ref return self Self static struct super trait true type unsafe use where while')
const shellDefinition = { ...language('Shell', 'case do done elif else esac export fi for function if in local readonly return then unset until while'), hashComments: true }
const sqlDefinition = { ...language('SQL', 'alter and as asc begin by case create delete desc distinct drop else end exists false from group having in index inner insert into is join left like limit not null on or order outer primary references right select set table true union unique update values where'), sqlComments: true }

const tokenPattern = /("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|`(?:\\.|[^`\\])*`|\/\/.*$|#.*$|--.*$|\b\d+(?:\.\d+)?\b|\b[A-Za-z_$][\w$]*\b)/g

export function DiffPreview({ diff, onClose }: { diff: FileDiff; onClose: () => void }) {
	const syntax = languageForPath(diff.path)
	const summary = diff.binary ? '二进制文件' : diff.truncated ? 'Diff 已截断' : 'Unified diff'
	return <div className="border-t bg-zinc-950 text-zinc-100">
		<div className="flex items-center justify-between px-4 py-2 text-xs">
			<span>{syntax.label} · {summary}</span>
			<Button variant="ghost" size="icon-sm" type="button" aria-label="关闭 Diff" onClick={onClose}><XIcon /></Button>
		</div>
		{diff.binary ? <p className="border-t border-white/10 p-4 font-mono text-xs text-zinc-400">不展示二进制内容</p> : (
			<div className="max-h-96 overflow-auto border-t border-white/10 py-2 font-mono text-xs leading-5" aria-label={`${diff.path} Diff`}>
				{diff.patch.split('\n').map((line, index) => <DiffLine key={`${index}-${line}`} line={line} index={index} syntax={syntax} />)}
			</div>
		)}
	</div>
}

function DiffLine({ line, index, syntax }: { line: string; index: number; syntax: LanguageDefinition }) {
	const kind = lineKind(line)
	const prefix = kind === 'addition' ? '+' : kind === 'deletion' ? '−' : ' '
	const content = kind === 'addition' || kind === 'deletion' ? line.slice(1) : line
	return <div className={`grid min-w-max grid-cols-[2rem_minmax(0,1fr)] px-4 ${lineTone(kind)}`} data-testid={`diff-${kind}-${index}`}>
		<span className="select-none text-center opacity-80" aria-hidden="true">{prefix}</span>
		<code className="pr-4 whitespace-pre">{kind === 'header' || kind === 'hunk' ? content || ' ' : highlight(content || ' ', syntax)}</code>
	</div>
}

function lineKind(line: string): DiffLineKind {
	if (line.startsWith('diff --git') || line.startsWith('index ') || line.startsWith('--- ') || line.startsWith('+++ ') || line.startsWith('new file ') || line.startsWith('deleted file ') || line.startsWith('rename ')) return 'header'
	if (line.startsWith('@@')) return 'hunk'
	if (line.startsWith('+')) return 'addition'
	if (line.startsWith('-')) return 'deletion'
	return 'context'
}

function lineTone(kind: DiffLineKind): string {
	if (kind === 'addition') return 'bg-emerald-950/35 text-emerald-100'
	if (kind === 'deletion') return 'bg-rose-950/35 text-rose-100'
	if (kind === 'hunk') return 'bg-sky-950/40 text-sky-300'
	if (kind === 'header') return 'bg-zinc-900 text-zinc-400'
	return 'text-zinc-200'
}

function highlight(source: string, syntax: LanguageDefinition): ReactNode[] {
	const result: ReactNode[] = []
	let cursor = 0
	for (const match of source.matchAll(tokenPattern)) {
		const start = match.index
		if (start > cursor) result.push(source.slice(cursor, start))
		result.push(<span className={tokenTone(match[0], syntax)} key={`${start}-${match[0]}`}>{match[0]}</span>)
		cursor = start + match[0].length
	}
	if (cursor < source.length) result.push(source.slice(cursor))
	return result
}

function tokenTone(token: string, syntax: LanguageDefinition): string | undefined {
	if ((syntax.slashComments && token.startsWith('//')) || (syntax.hashComments && token.startsWith('#')) || (syntax.sqlComments && token.startsWith('--'))) return 'text-zinc-500 italic'
	if (/^["'`]/.test(token)) return 'text-amber-300'
	if (/^\d/.test(token)) return 'text-sky-300'
	if (syntax.keywords.has(token.toLowerCase())) return 'text-violet-300'
	return undefined
}

function languageForPath(path: string): LanguageDefinition {
	const extension = path.split('.').pop()?.toLowerCase() ?? ''
	if (extension === 'go') return { ...goDefinition, slashComments: true }
	if (['ts', 'tsx'].includes(extension)) return { ...typeScriptDefinition, slashComments: true }
	if (['js', 'jsx', 'mjs', 'cjs'].includes(extension)) return { ...javaScriptDefinition, slashComments: true }
	if (extension === 'json') return jsonDefinition
	if (extension === 'py') return pythonDefinition
	if (extension === 'rs') return { ...rustDefinition, slashComments: true }
	if (['sh', 'bash', 'zsh'].includes(extension)) return shellDefinition
	if (extension === 'sql') return sqlDefinition
	return language(extension ? extension.toUpperCase() : 'Text', '')
}

function language(label: string, keywords: string): LanguageDefinition {
	return { label, keywords: new Set(keywords.split(' ').filter(Boolean).map((keyword) => keyword.toLowerCase())) }
}
