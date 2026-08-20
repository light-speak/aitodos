import { useMemo, useState } from 'react'
import { AlertCircleIcon, GitBranchIcon, TagIcon } from 'lucide-react'

import { errorMessage } from '../api/client'
import { shortGitSHA } from '../lib/utils'
import type { CreateReleaseInput, Release, RepositoryInfo } from '../types'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from './ui/dialog'
import { Input } from './ui/input'
import { Label } from './ui/label'

interface ReleaseDialogProps {
  repository: RepositoryInfo
  releases: Release[]
  submitting: boolean
  onClose: () => void
  onCreate: (input: CreateReleaseInput) => Promise<Release>
}

export function ReleaseDialog({ repository, releases, submitting, onClose, onCreate }: ReleaseDialogProps) {
  const initialBranch = repository.current_branch || repository.branches[0]?.name || ''
  const [version, setVersion] = useState('')
  const [sourceBranch, setSourceBranch] = useState(initialBranch)
  const [formError, setFormError] = useState('')
  const selectedBranch = useMemo(
    () => repository.branches.find((branch) => branch.name === sourceBranch),
    [repository.branches, sourceBranch],
  )

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault()
    setFormError('')
    if (version.trim() === '' || sourceBranch === '') {
      setFormError('请输入 SemVer 版本并选择来源分支')
      return
    }
    try {
      const created = await onCreate({ version, source_branch: sourceBranch, task_ids: [] })
      setVersion('')
      setSourceBranch(created.source_branch)
    } catch (createError: unknown) {
      setFormError(errorMessage(createError))
    }
  }

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="max-h-[min(48rem,calc(100vh-3rem))] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Releases</DialogTitle>
          <DialogDescription>
            Release 固定到来源分支当前的 Commit，并创建本地 annotated tag；不会 push，也不会包含未提交内容。
          </DialogDescription>
        </DialogHeader>

        <form className="grid gap-4" onSubmit={(event) => { void handleSubmit(event) }}>
			{!repository.has_head ? <p className="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800">仓库尚无 Commit。先创建首个 Commit，才能创建 Release Tag。</p> : null}
          <div className="rounded-xl border bg-muted/30 p-4">
            <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)]">
              <div className="grid gap-2">
                <Label htmlFor="release-version">版本</Label>
                <div className="flex items-center rounded-lg border bg-background focus-within:ring-3 focus-within:ring-ring/40">
                  <span className="pl-3 font-mono text-sm text-muted-foreground">v</span>
                  <Input
                    id="release-version"
                    className="border-0 font-mono shadow-none focus-visible:ring-0"
                    placeholder="1.0.1"
                    value={version}
                    onChange={(event) => setVersion(event.target.value)}
                  />
                </div>
              </div>
              <div className="grid gap-2">
                <Label htmlFor="release-branch">来源分支</Label>
                <select
                  id="release-branch"
                  className="h-8 w-full rounded-lg border bg-background px-2.5 font-mono text-sm outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
                  value={sourceBranch}
                  onChange={(event) => setSourceBranch(event.target.value)}
                >
                  {repository.branches.map((branch) => <option value={branch.name} key={branch.name}>{branch.name}</option>)}
                </select>
              </div>
            </div>
            <div className="mt-3 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
              <GitBranchIcon className="size-3.5" />
              <span className="font-mono">{sourceBranch || '未选择'} @ {shortGitSHA(selectedBranch?.head_sha ?? '')}</span>
              {sourceBranch === repository.current_branch && repository.dirty ? (
                <Badge variant="outline" className="border-amber-300 bg-amber-50 text-amber-700">主工作区有未提交修改</Badge>
              ) : null}
            </div>
            <p className="mt-3 text-xs leading-5 text-muted-foreground">
              只包含来源分支上已经提交的内容。Task Workspace 中未提交或尚未合入该分支的修改不会进入 Release。
            </p>
          </div>

          {formError ? (
            <p className="flex items-center gap-2 text-sm text-destructive" role="alert">
              <AlertCircleIcon className="size-4" />{formError}
            </p>
          ) : null}

          <section>
            <h3 className="mb-2 text-xs font-medium tracking-wide text-muted-foreground uppercase">发布历史</h3>
            {releases.length === 0 ? (
              <div className="rounded-xl border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">尚未创建 Release</div>
            ) : (
              <div className="grid gap-2">
                {releases.map((item) => (
                  <div className="flex items-center gap-3 rounded-xl border px-3 py-2.5" key={item.id}>
                    <span className="flex size-8 items-center justify-center rounded-lg bg-muted"><TagIcon className="size-4" /></span>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span className="font-mono text-sm font-medium">{item.tag_name}</span>
                        <Badge variant={item.status === 'TAGGED' ? 'secondary' : 'outline'}>{releaseStatusLabel(item)}</Badge>
						{item.task_ids.length > 0 ? <Badge variant="outline">自动关联 {item.task_ids.length} 个 Task</Badge> : null}
                      </div>
                      <p className="truncate font-mono text-xs text-muted-foreground">
                        {item.source_branch} @ {shortGitSHA(item.commit_sha)}
                      </p>
										{item.failure_message ? <p className="mt-1 text-xs text-destructive">{item.failure_message}</p> : null}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </section>

          <DialogFooter>
            <Button variant="outline" type="button" onClick={onClose}>关闭</Button>
            <Button type="submit" disabled={submitting || !repository.has_head || repository.branches.length === 0}>
              <TagIcon />{submitting ? '正在创建…' : '创建本地 Tag'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function releaseStatusLabel(item: Release): string {
  if (item.status === 'TAGGED') return '已创建'
  if (item.status === 'FAILED') return '失败'
  return '创建中'
}
