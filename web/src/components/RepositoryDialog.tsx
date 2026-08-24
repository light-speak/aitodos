import { GitBranchIcon, GitCommitHorizontalIcon, NetworkIcon, UserRoundIcon } from 'lucide-react'

import { shortGitSHA } from '../lib/utils'
import type { RepositoryInfo } from '../types'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from './ui/dialog'

interface RepositoryDialogProps {
	repository: RepositoryInfo
	onClose: () => void
}

export function RepositoryDialog({ repository, onClose }: RepositoryDialogProps) {
	return (
		<Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
			<DialogContent className="max-h-[min(48rem,calc(100vh-3rem))] overflow-y-auto sm:max-w-3xl">
				<DialogHeader>
					<DialogTitle>Git 仓库</DialogTitle>
					<DialogDescription>以下是本地 Git 事实，不会自动 fetch、push 或修改分支。</DialogDescription>
				</DialogHeader>

				<div className="grid gap-4">
					<section className="grid gap-3 rounded-xl border bg-muted/20 p-4 text-sm">
						<RepositoryFact label="仓库根目录" value={repository.root} />
						<RepositoryFact label="Git Common Dir" value={repository.git_common_dir} />
						<RepositoryFact label="Git 版本" value={repository.git_version || '未知'} />
						<RepositoryFact label="Git 身份" value={repository.identity_configured ? `${repository.user_name} <${repository.user_email}>` : '未配置'} />
					</section>

					<section className="grid gap-3 rounded-xl border p-4 text-sm">
						<div className="flex flex-wrap items-center gap-2">
							<GitBranchIcon className="size-4 text-muted-foreground" />
							<span className="font-mono font-medium">{repository.current_branch || 'detached HEAD'}</span>
							<Badge variant="outline">默认 {repository.default_branch || '未配置'}</Badge>
							{repository.dirty ? <Badge variant="outline" className="border-amber-300 bg-amber-50 text-amber-700">主工作区有修改</Badge> : <Badge variant="outline" className="border-emerald-300 bg-emerald-50 text-emerald-700">主工作区干净</Badge>}
						</div>
						<RepositoryFact icon={<GitCommitHorizontalIcon />} label="HEAD" value={repository.has_head ? repository.head_sha : '尚无 Commit'} />
						<RepositoryFact label="远端默认分支" value={repository.remote_default_branch || '未知'} />
						<RepositoryFact label="Upstream" value={repository.upstream || '未配置'} />
						<RepositoryFact label="同步状态" value={trackingSummary(repository)} />
					</section>

					<RemoteList repository={repository} />
					<BranchList repository={repository} />
				</div>

				<DialogFooter><Button type="button" onClick={onClose}>关闭</Button></DialogFooter>
			</DialogContent>
		</Dialog>
	)
}

function RemoteList({ repository }: { repository: RepositoryInfo }) {
	return (
		<section>
			<h3 className="mb-2 flex items-center gap-2 text-xs font-medium tracking-wide text-muted-foreground uppercase"><NetworkIcon className="size-4" />Remotes</h3>
			{repository.remotes.length === 0 ? <p className="rounded-xl border border-dashed p-4 text-sm text-muted-foreground">未配置 Remote</p> : (
				<div className="grid gap-2">
					{repository.remotes.map((remote) => (
						<div className="grid gap-1 rounded-xl border px-4 py-3 text-sm" key={remote.name}>
							<span className="font-mono font-medium">{remote.name}</span>
							<span className="grid grid-cols-[44px_minmax(0,1fr)] gap-2 text-xs text-muted-foreground"><span>Fetch</span><span className="break-all font-mono">{remote.fetch_url}</span></span>
							<span className="grid grid-cols-[44px_minmax(0,1fr)] gap-2 text-xs text-muted-foreground"><span>Push</span><span className="break-all font-mono">{remote.push_url}</span></span>
						</div>
					))}
				</div>
			)}
		</section>
	)
}

function BranchList({ repository }: { repository: RepositoryInfo }) {
	return (
		<section>
			<h3 className="mb-2 flex items-center gap-2 text-xs font-medium tracking-wide text-muted-foreground uppercase"><GitBranchIcon className="size-4" />本地分支</h3>
			<div className="grid gap-2">
				{repository.branches.map((branch) => (
					<div className="flex items-center gap-3 rounded-xl border px-4 py-2.5 text-sm" key={branch.name}>
						<span className="min-w-0 flex-1 truncate font-mono">{branch.name}</span>
						{branch.name === repository.default_branch ? <Badge variant="secondary">默认</Badge> : null}
						<span className="font-mono text-xs text-muted-foreground">{shortGitSHA(branch.head_sha)}</span>
					</div>
				))}
			</div>
		</section>
	)
}

function RepositoryFact(props: { icon?: React.ReactNode; label: string; value: string }) {
	return (
		<div className="grid grid-cols-[120px_minmax(0,1fr)] items-start gap-3">
			<span className="flex items-center gap-1.5 text-muted-foreground">{props.icon ?? <UserRoundIcon className="size-3.5 opacity-0" />}{props.label}</span>
			<span className="break-all font-mono text-xs leading-5">{props.value}</span>
		</div>
	)
}

function trackingSummary(repository: RepositoryInfo): string {
	if (!repository.upstream || repository.ahead === undefined || repository.behind === undefined) return '未知（尚未配置 Upstream）'
	return `领先 ${repository.ahead} · 落后 ${repository.behind}`
}
