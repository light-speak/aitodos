import { useState } from 'react'
import type { FormEvent } from 'react'
import { AlertCircleIcon, BotIcon, LockKeyholeIcon, PencilIcon, RefreshCwIcon } from 'lucide-react'

import { errorMessage } from '../api/client'
import type { AgentProfile, AgentProfileRevisionInput, ProjectCapabilityCatalog, WorkspacePolicy } from '../types'
import { MarkdownTextarea } from './MarkdownTextarea'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from './ui/card'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from './ui/dialog'
import { Input } from './ui/input'
import { Label } from './ui/label'
import { Skeleton } from './ui/skeleton'
import { Textarea } from './ui/textarea'

interface AgentProfilesPageProps {
	profiles: AgentProfile[]
	loading: boolean
	error: unknown
	saving: boolean
	capabilities: ProjectCapabilityCatalog
	onReload: () => void
	onSave: (profileID: string, input: AgentProfileRevisionInput) => Promise<void>
	onConfigureDefaults?: () => Promise<void>
}

export function AgentProfilesPage(props: AgentProfilesPageProps) {
	const [editing, setEditing] = useState<AgentProfile | null>(null)
	const [setupError, setSetupError] = useState('')
	if (props.loading) return <div className="grid gap-4 px-4 sm:px-6 lg:grid-cols-2 lg:px-8">{[1, 2, 3, 4].map((item) => <Skeleton className="h-64 rounded-xl" key={item} />)}</div>
	if (props.error) return <PageError error={props.error} onReload={props.onReload} />
	const unconfigured = props.profiles.filter((profile) => !profile.current_revision.command).length
	async function configureDefaults() {
		if (!props.onConfigureDefaults) return
		setSetupError('')
		try { await props.onConfigureDefaults() } catch (error: unknown) { setSetupError(errorMessage(error)) }
	}
	return <>{unconfigured > 0 && props.onConfigureDefaults ? <section className="mx-4 mb-4 flex items-center gap-4 rounded-xl border bg-muted/20 p-4 sm:mx-6 lg:mx-8" aria-label="Agent 初始化"><div className="min-w-0 flex-1"><p className="font-medium">还有 {unconfigured} 个 Agent 未配置</p><p className="text-sm text-muted-foreground">使用当前 PATH 中的 Codex，一次创建全部默认配置；模型沿用 CLI 默认值。</p>{setupError ? <p className="mt-1 text-sm text-destructive" role="alert">{setupError}</p> : null}</div><Button type="button" disabled={props.saving} onClick={() => { void configureDefaults() }}>{props.saving ? '配置中…' : '一键配置 Codex'}</Button></section> : null}<section className="grid gap-4 px-4 pb-8 sm:px-6 lg:grid-cols-2 lg:px-8" aria-label="Agent Profiles">{props.profiles.map((profile) => <ProfileCard profile={profile} onEdit={() => setEditing(profile)} key={profile.id} />)}</section>{editing ? <ProfileDialog profile={editing} capabilities={props.capabilities} saving={props.saving} onClose={() => setEditing(null)} onSave={async (input) => { await props.onSave(editing.id, input); setEditing(null) }} /> : null}</>
}

function ProfileCard({ profile, onEdit }: { profile: AgentProfile; onEdit: () => void }) {
	const revision = profile.current_revision
	const capabilityCount = revision.tool_policy.skills.length + revision.tool_policy.mcp_servers.length
	return <Card><CardHeader><div className="flex items-start justify-between gap-3"><div className="flex items-center gap-3"><span className="flex size-10 items-center justify-center rounded-xl bg-muted"><BotIcon className="size-5" /></span><div><CardTitle>{profile.name}</CardTitle><CardDescription>{roleDescription(profile.role)}</CardDescription></div></div><Badge variant={revision.command ? 'secondary' : 'outline'}>{revision.command ? `Revision ${revision.revision}` : '未配置'}</Badge></div></CardHeader><CardContent className="grid gap-4"><p className="line-clamp-3 text-sm leading-6">{revision.instructions}</p><Fact label="运行" value={revision.command ? `${revision.command}${revision.model ? ` · ${revision.model}` : ' · CLI 默认模型'}` : '尚未设置命令'} /><Fact label="项目能力" value={capabilityCount > 0 ? `${revision.tool_policy.skills.length} Skills · ${revision.tool_policy.mcp_servers.length} MCP` : '未选择'} /><p className="flex items-center gap-2 rounded-lg border bg-muted/30 px-3 py-2 text-xs"><LockKeyholeIcon className="size-3.5" />{workspaceLabel(revision.workspace_policy)}</p></CardContent><CardFooter><Button variant="outline" onClick={onEdit} aria-label={`配置${profile.name}`}><PencilIcon />配置</Button></CardFooter></Card>
}

function ProfileDialog(props: { profile: AgentProfile; capabilities: ProjectCapabilityCatalog; saving: boolean; onClose: () => void; onSave: (input: AgentProfileRevisionInput) => Promise<void> }) {
	const current = props.profile.current_revision
	const [input, setInput] = useState<AgentProfileRevisionInput>(revisionInput(current))
	const [argsText, setArgsText] = useState(current.args.join('\n'))
	const [error, setError] = useState('')
	function applyCodexDefaults() {
		setInput({ ...input, adapter: 'codex-app-server', command: 'codex', model: input.model || 'gpt-5.3-codex' })
		setArgsText('')
	}
	async function submit(event: FormEvent<HTMLFormElement>) {
		event.preventDefault(); setError('')
		try { await props.onSave({ ...input, args: argsText.split('\n').map((item) => item.trim()).filter(Boolean) }) } catch (submitError: unknown) { setError(errorMessage(submitError)) }
	}
	return <Dialog open onOpenChange={(open) => { if (!open) props.onClose() }}><DialogContent className="max-h-[calc(100svh-2rem)] overflow-y-auto sm:max-w-2xl"><DialogHeader><DialogTitle>配置{props.profile.name}</DialogTitle><DialogDescription>保存会创建 Revision {current.revision + 1}，只影响未来 Run。职责提示词可以调整，Workspace 与审批权限由系统固定。</DialogDescription></DialogHeader><form className="grid gap-5" onSubmit={(event) => { void submit(event) }}><div className="flex justify-end"><Button type="button" variant="outline" size="sm" onClick={applyCodexDefaults}>填入 Codex 推荐配置</Button></div><Field label="职责提示词"><MarkdownTextarea value={input.instructions} rows={7} onChange={(instructions) => setInput({ ...input, instructions })} /></Field><div className="grid gap-4 sm:grid-cols-2"><TextField id="agent-command" label="Agent 命令" value={input.command} onChange={(command) => setInput({ ...input, command })} /><TextField id="agent-model" label="模型（可留空）" value={input.model} placeholder="例如 gpt-5.3-codex" onChange={(model) => setInput({ ...input, model })} /></div><p className="-mt-2 text-xs leading-5 text-muted-foreground">模型是自由填写项，便于跟随 Codex 更新；例如填 <code>gpt-5.3-codex</code>。留空时沿用 Codex CLI 默认模型。</p><CapabilityPolicyEditor catalog={props.capabilities} input={input} onChange={setInput} /><details className="rounded-xl border bg-muted/20 p-4"><summary className="cursor-pointer font-medium">高级配置</summary><div className="mt-4 grid gap-4"><TextField id="agent-adapter" label="Adapter" value={input.adapter} onChange={(adapter) => setInput({ ...input, adapter })} /><p className="-mt-2 text-xs leading-5 text-muted-foreground">推荐使用 <code>codex-app-server</code>，这样命令、文件、网络权限可直接回到面板审批；<code>codex</code> 保留给旧的单次 CLI 配置。</p><div className="grid gap-2"><Label htmlFor="agent-args">额外参数（每行一项）</Label><Textarea id="agent-args" className="min-h-28 resize-y font-mono text-xs leading-5" value={argsText} onChange={(event) => setArgsText(event.currentTarget.value)} /><p className="text-xs leading-5 text-muted-foreground">App Server 推荐留空。仅在使用 generic 或旧 codex adapter 时填写命令参数；支持 {'{prompt_file}'}、{'{result_file}'}、{'{workspace}'}、{'{model}'}、{'{run_id}'}。</p></div><div className="grid gap-4 sm:grid-cols-3"><NumberField id="timeout" label="超时（秒）" value={input.timeout_seconds} onChange={(timeout_seconds) => setInput({ ...input, timeout_seconds })} /><NumberField id="recent-limit" label="近期消息数" value={input.recent_message_limit} onChange={(recent_message_limit) => setInput({ ...input, recent_message_limit })} /><NumberField id="retrieval-limit" label="检索结果数" value={input.retrieval_limit} onChange={(retrieval_limit) => setInput({ ...input, retrieval_limit })} /></div><p className="text-xs leading-5 text-muted-foreground">上下文由系统自动组织。系统会完整保留当前任务、验收标准、安全规则、项目规则和阻塞信息，只对重复内容及低价值历史做去重或省略；内部预算只作软保护，不会因超出估算而拒绝执行。</p></div></details>{error ? <p className="text-sm text-destructive" role="alert">{error}</p> : null}<DialogFooter><Button variant="outline" type="button" onClick={props.onClose}>取消</Button><Button type="submit" disabled={props.saving}>{props.saving ? '保存中…' : `保存为 Revision ${current.revision + 1}`}</Button></DialogFooter></form></DialogContent></Dialog>
}

function revisionInput(revision: AgentProfile['current_revision']): AgentProfileRevisionInput {
	return { instructions: revision.instructions, adapter: revision.adapter, command: revision.command, args: [...revision.args], model: revision.model, max_input_tokens: revision.max_input_tokens, reserved_output_tokens: revision.reserved_output_tokens, recent_message_limit: revision.recent_message_limit, retrieval_limit: revision.retrieval_limit, timeout_seconds: revision.timeout_seconds, tool_policy: { skills: revision.tool_policy.skills.map((binding) => ({ ...binding })), mcp_servers: revision.tool_policy.mcp_servers.map((binding) => ({ ...binding, enabled_tools: [...binding.enabled_tools] })) } }
}

function CapabilityPolicyEditor({ catalog, input, onChange }: { catalog: ProjectCapabilityCatalog; input: AgentProfileRevisionInput; onChange: (input: AgentProfileRevisionInput) => void }) {
	function setSkill(skillID: string, selected: boolean) {
		const skills = selected ? [...input.tool_policy.skills, { skill_id: skillID, required: false }] : input.tool_policy.skills.filter((item) => item.skill_id !== skillID)
		onChange({ ...input, tool_policy: { ...input.tool_policy, skills } })
	}
	function setMCP(serverID: string, selected: boolean) {
		const mcp_servers = selected ? [...input.tool_policy.mcp_servers, { server_id: serverID, required: false, enabled_tools: [] }] : input.tool_policy.mcp_servers.filter((item) => item.server_id !== serverID)
		onChange({ ...input, adapter: selected ? 'codex-app-server' : input.adapter, tool_policy: { ...input.tool_policy, mcp_servers } })
	}
	return <div className="grid gap-3 rounded-xl border p-4"><div><p className="font-medium">项目能力</p><p className="text-xs leading-5 text-muted-foreground">未选择的 MCP 会在该 Run 中禁用；必需能力不可用时不会调用模型。</p></div>{catalog.skills.length === 0 && catalog.mcp_servers.length === 0 ? <p className="text-sm text-muted-foreground">请先在 Agent 页面顶部登记 Skill 或 MCP。</p> : null}<div className="grid gap-2">{catalog.skills.map((skill) => { const binding = input.tool_policy.skills.find((item) => item.skill_id === skill.id); return <CapabilityRow key={skill.id} label={`Skill · ${skill.name}`} detail={skill.source_path} selected={binding !== undefined} required={binding?.required ?? false} onSelect={(selected) => setSkill(skill.id, selected)} onRequired={(required) => onChange({ ...input, tool_policy: { ...input.tool_policy, skills: input.tool_policy.skills.map((item) => item.skill_id === skill.id ? { ...item, required } : item) } })} /> })}{catalog.mcp_servers.map((server) => { const binding = input.tool_policy.mcp_servers.find((item) => item.server_id === server.id); return <div className="rounded-lg border p-3" key={server.id}><CapabilityRow label={`MCP · ${server.name}`} detail={server.config_name} selected={binding !== undefined} required={binding?.required ?? false} onSelect={(selected) => setMCP(server.id, selected)} onRequired={(required) => onChange({ ...input, tool_policy: { ...input.tool_policy, mcp_servers: input.tool_policy.mcp_servers.map((item) => item.server_id === server.id ? { ...item, required } : item) } })} />{binding ? <MCPToolsField serverID={server.id} binding={binding} input={input} onChange={onChange} /> : null}</div> })}</div></div>
}

function MCPToolsField({ serverID, binding, input, onChange }: { serverID: string; binding: AgentProfileRevisionInput['tool_policy']['mcp_servers'][number]; input: AgentProfileRevisionInput; onChange: (input: AgentProfileRevisionInput) => void }) {
	const [draft, setDraft] = useState(binding.enabled_tools.join(', '))
	function update(value: string) {
		setDraft(value)
		const enabled_tools = value.split(',').map((tool) => tool.trim()).filter(Boolean)
		onChange({ ...input, tool_policy: { ...input.tool_policy, mcp_servers: input.tool_policy.mcp_servers.map((item) => item.server_id === serverID ? { ...item, enabled_tools } : item) } })
	}
	return <TextField id={`mcp-tools-${serverID}`} label="允许的 Tool（逗号分隔，留空表示该 Server 全部 Tool）" value={draft} onChange={update} />
}

function CapabilityRow({ label, detail, selected, required, onSelect, onRequired }: { label: string; detail: string; selected: boolean; required: boolean; onSelect: (selected: boolean) => void; onRequired: (required: boolean) => void }) {
	return <div className="flex items-center gap-3 py-1"><label className="flex min-w-0 flex-1 items-center gap-2 text-sm"><input aria-label={`允许 ${label}`} type="checkbox" checked={selected} onChange={(event) => onSelect(event.currentTarget.checked)} /><span className="min-w-0"><span className="block font-medium">{label}</span><span className="block truncate font-mono text-xs text-muted-foreground">{detail}</span></span></label>{selected ? <label className="flex items-center gap-2 text-xs"><input aria-label={`${label} 必需`} type="checkbox" checked={required} onChange={(event) => onRequired(event.currentTarget.checked)} />必需</label> : null}</div>
}

function Field({ label, children }: { label: string; children: React.ReactNode }) { return <div className="grid gap-2"><Label>{label}</Label>{children}</div> }
function TextField({ id, label, value, placeholder, onChange }: { id: string; label: string; value: string; placeholder?: string; onChange: (value: string) => void }) { return <div className="grid gap-2"><Label htmlFor={id}>{label}</Label><Input id={id} value={value} placeholder={placeholder} onChange={(event) => onChange(event.currentTarget.value)} /></div> }
function NumberField({ id, label, value, onChange }: { id: string; label: string; value: number; onChange: (value: number) => void }) { return <div className="grid gap-2"><Label htmlFor={id}>{label}</Label><Input id={id} type="number" value={value} onChange={(event) => onChange(Number(event.currentTarget.value))} /></div> }
function Fact({ label, value }: { label: string; value: string }) { return <div><p className="text-muted-foreground">{label}</p><p className="mt-1 truncate font-mono">{value}</p></div> }
function PageError({ error, onReload }: { error: unknown; onReload: () => void }) { return <div className="px-4 sm:px-6 lg:px-8"><div className="flex items-center gap-3 rounded-xl border border-destructive/20 bg-destructive/5 p-4 text-sm text-destructive"><AlertCircleIcon className="size-4" />{errorMessage(error)}<Button className="ml-auto" variant="ghost" size="sm" onClick={onReload}><RefreshCwIcon />重试</Button></div></div> }
function roleDescription(role: AgentProfile['role']): string { return ({ PLANNER: '分析 Topic 并生成可审核计划', TRIAGER: '生成 Task 标题和复杂度评估', IMPLEMENTER: '实现已批准的正式 Task', REVISION: '处理驳回意见和回归', REVIEWER: '只读检查 Diff 与证据' })[role] }
function workspaceLabel(policy: WorkspacePolicy): string { return ({ NONE: '不提供 Task Workspace', READ_ONLY: 'Task Workspace 只读', WRITE_TASK: '仅当前 Task Workspace 可写' })[policy] }
