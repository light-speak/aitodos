import { useState } from 'react'
import type { FormEvent } from 'react'
import { PlugIcon, RefreshCwIcon, SparklesIcon } from 'lucide-react'

import { errorMessage } from '../api/client'
import type { ProjectCapabilityCatalog } from '../types'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from './ui/card'
import { Input } from './ui/input'
import { Label } from './ui/label'
import { Skeleton } from './ui/skeleton'

interface ProjectCapabilitiesPanelProps {
	catalog: ProjectCapabilityCatalog
	loading: boolean
	adding: boolean
	error: unknown
	onReload: () => void
	onAddSkill: (input: { name: string; source_path: string }) => Promise<void>
	onRefreshSkill: (skillID: string, version: number) => Promise<void>
	onAddMCPServer: (input: { name: string; config_name: string }) => Promise<void>
}

export function ProjectCapabilitiesPanel(props: ProjectCapabilitiesPanelProps) {
	const [skill, setSkill] = useState({ name: '', source_path: '' })
	const [mcp, setMCP] = useState({ name: '', config_name: '' })
	const [localError, setLocalError] = useState('')
	async function submit(event: FormEvent, action: () => Promise<void>, clear: () => void) {
		event.preventDefault(); setLocalError('')
		try { await action(); clear() } catch (cause: unknown) { setLocalError(errorMessage(cause)) }
	}
	if (props.loading) return <Skeleton className="mx-4 mb-6 h-56 rounded-xl sm:mx-6 lg:mx-8" />
	return <section className="px-4 pb-6 sm:px-6 lg:px-8" aria-label="项目能力">
		<div className="mb-3 flex items-end justify-between gap-3"><div><h3 className="font-heading text-lg font-semibold">项目能力</h3><p className="text-sm text-muted-foreground">只登记引用和哈希，不保存 MCP Token；Agent 需要在 Revision 中明确选择。</p></div>{props.error ? <Button variant="outline" size="sm" onClick={props.onReload}><RefreshCwIcon />重试</Button> : null}</div>
		{props.error || localError ? <p className="mb-3 text-sm text-destructive" role="alert">{localError || errorMessage(props.error)}</p> : null}
		<div className="grid gap-4 lg:grid-cols-2">
			<Card><CardHeader><CardTitle className="flex items-center gap-2"><SparklesIcon className="size-4" />Skills</CardTitle><CardDescription>路径指向包含 SKILL.md 的目录；相对路径限制在当前项目内。</CardDescription></CardHeader><CardContent className="grid gap-4"><CapabilityList items={props.catalog.skills.map((item) => ({ id: item.id, name: item.name, detail: `${item.source_path} · v${item.version} · ${item.content_sha256.slice(0, 12)}`, actionLabel: `重新校验 ${item.name}`, onAction: () => props.onRefreshSkill(item.id, item.version) }))} empty="尚未登记 Skill" disabled={props.adding} /><form className="grid gap-3" onSubmit={(event) => { void submit(event, () => props.onAddSkill(skill), () => setSkill({ name: '', source_path: '' })) }}><Field id="skill-name" label="Skill 名称" value={skill.name} onChange={(name) => setSkill({ ...skill, name })} /><Field id="skill-path" label="Skill 路径" value={skill.source_path} placeholder=".agents/skills/testing" onChange={(source_path) => setSkill({ ...skill, source_path })} /><Button type="submit" variant="outline" disabled={props.adding || !skill.name.trim() || !skill.source_path.trim()}>添加 Skill</Button></form></CardContent></Card>
			<Card><CardHeader><CardTitle className="flex items-center gap-2"><PlugIcon className="size-4" />MCP Servers</CardTitle><CardDescription>引用本机 Codex 已配置的名称；连接地址和密钥仍由 Codex 管理。</CardDescription></CardHeader><CardContent className="grid gap-4"><CapabilityList items={props.catalog.mcp_servers.map((item) => ({ id: item.id, name: item.name, detail: item.config_name }))} empty="尚未登记 MCP" /><form className="grid gap-3" onSubmit={(event) => { void submit(event, () => props.onAddMCPServer(mcp), () => setMCP({ name: '', config_name: '' })) }}><Field id="mcp-name" label="MCP 名称" value={mcp.name} onChange={(name) => setMCP({ ...mcp, name })} /><Field id="mcp-config-name" label="Codex MCP 配置名" value={mcp.config_name} placeholder="playwright" onChange={(config_name) => setMCP({ ...mcp, config_name })} /><Button type="submit" variant="outline" disabled={props.adding || !mcp.name.trim() || !mcp.config_name.trim()}>添加 MCP</Button></form></CardContent></Card>
		</div>
	</section>
}

function CapabilityList({ items, empty, disabled = false }: { items: Array<{ id: string; name: string; detail: string; actionLabel?: string; onAction?: () => Promise<void> }>; empty: string; disabled?: boolean }) {
	if (items.length === 0) return <p className="rounded-lg border border-dashed p-3 text-sm text-muted-foreground">{empty}</p>
	return <div className="grid gap-2">{items.map((item) => <div className="flex items-center gap-3 rounded-lg border p-3" key={item.id}><Badge variant="secondary">已启用</Badge><div className="min-w-0 flex-1"><p className="text-sm font-medium">{item.name}</p><p className="truncate font-mono text-xs text-muted-foreground">{item.detail}</p></div>{item.onAction && item.actionLabel ? <Button type="button" variant="ghost" size="sm" disabled={disabled} aria-label={item.actionLabel} onClick={() => { if (item.onAction) void item.onAction().catch(() => undefined) }}><RefreshCwIcon />校验</Button> : null}</div>)}</div>
}

function Field({ id, label, value, placeholder, onChange }: { id: string; label: string; value: string; placeholder?: string; onChange: (value: string) => void }) {
	return <div className="grid gap-2"><Label htmlFor={id}>{label}</Label><Input id={id} value={value} placeholder={placeholder} onChange={(event) => onChange(event.currentTarget.value)} /></div>
}
