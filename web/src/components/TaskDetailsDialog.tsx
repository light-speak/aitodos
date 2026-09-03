import { lazy, Suspense } from 'react'
import { Clock3Icon, ListChecksIcon, LoaderCircleIcon, TextIcon, XIcon } from 'lucide-react'

import type { AgentRun, Clarification, ClarificationAnswerInput, CreateTaskEstimateInput, CreateTaskTestCaseInput, CreateTaskTestResultInput, DiscussionMessage, RepositoryInfo, RunPurpose, Task, TaskAssessmentState, TaskAssociation, TaskFeedback, TaskFeedbackIntent, TaskQuality, TaskRelationType, TaskStatus, Topic, TopicAssociation, UpdateTaskDetailsInput, Workspace } from '../types'
import { TaskClarificationsPanel } from './ClarificationsPanel'
import { DiscussionComposer, DiscussionMessages } from './DiscussionPanel'
import { MarkdownContent } from './MarkdownContent'
import { RelatedTasks } from './RelatedTasks'
import { RelatedTopics } from './RelatedTopics'
import { TaskWorkspacePanel } from './TaskWorkspacePanel'
import { TaskQualityPanel } from './TaskQualityPanel'
import { TaskAssessmentPanel } from './TaskAssessmentPanel'
import { TaskRunsPanel } from './TaskRunsPanel'
import { TaskRequirementsEditor } from './TaskRequirementsEditor'
import { SubjectKnowledgePanel } from './SubjectKnowledgePanel'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Separator } from './ui/separator'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from './ui/sheet'

const TaskChangesPanel = lazy(async () => {
	const module = await import('./TaskChangesPanel')
	return { default: module.TaskChangesPanel }
})

interface TaskDetailsDialogProps {
  task: Task
	activeRun: AgentRun | null
  tasks: Task[]
  messages: DiscussionMessage[]
	feedback: TaskFeedback[]
  associations: TaskAssociation[]
  topics: Topic[]
  topicAssociations: TopicAssociation[]
  discussionLoading: boolean
  relationLoading: boolean
  submitting: boolean
	retryingFeedbackID: string | null
  discussionError: unknown
  relationError: unknown
  pendingRelationTaskIDs: Set<string>
  topicRelationLoading: boolean
  topicRelationError: unknown
  pendingRelationTopicIDs: Set<string>
  workspace: Workspace | null
  workspaceLoading: boolean
	workspaceError: unknown
	quality: TaskQuality | null
	qualityLoading: boolean
	qualityError: unknown
	qualityBusy: boolean
	assessment: TaskAssessmentState | null
	assessmentLoading: boolean
	assessmentError: unknown
	assessmentBusy: boolean
	repository: RepositoryInfo | null
	clarifications: Clarification[]
	clarificationError: unknown
	answeringClarificationID: string | null
  onClose: () => void
  onReloadDiscussion: () => void
	onRetryFeedback: (feedbackID: string) => Promise<void>
  onSendMessage: (content: string, linkedTaskIDs: string[], intent?: TaskFeedbackIntent) => Promise<void>
  onAddRelation: (taskID: string, relationType?: TaskRelationType) => Promise<void>
  onRemoveRelation: (association: TaskAssociation) => Promise<void>
  onOpenTask: (taskID: string) => void
  onAddTopicRelation: (topicID: string) => Promise<void>
  onRemoveTopicRelation: (topicID: string) => Promise<void>
  onOpenTopic: (topicID: string) => void
	onTaskUpdated: (task: Task) => void
	onWorkspaceChanged: () => void
	onReloadQuality: () => void
	onCreateEstimate: (input: CreateTaskEstimateInput) => Promise<void>
	onCreateTestCase: (input: CreateTaskTestCaseInput) => Promise<void>
	onRecordTestResult: (testCaseID: string, input: CreateTaskTestResultInput) => Promise<void>
	onReloadAssessment: () => void
	onUpdateTitle: (title: string) => Promise<void>
	onUpdateTargetBranch: (targetBranch: string) => Promise<void>
	onUpdateDetails: (input: UpdateTaskDetailsInput) => Promise<void>
	onCancelTask: () => Promise<void>
	onArchiveTask: () => Promise<void>
	onReloadClarifications: () => void
	onAnswerClarification: (item: Clarification, input: Omit<ClarificationAnswerInput, 'expected_version'>) => Promise<void>
}

const statusLabels: Record<TaskStatus, string> = {
	BACKLOG: '待完善', READY: '等待执行', RUNNING: '执行中', REVIEW: '待验收',
  ACCEPTED: '已验收', CHANGES_REQUESTED: '需修改', BLOCKED: '已阻塞', CANCELLED: '已取消',
}

const statusTone: Record<TaskStatus, string> = {
  BACKLOG: 'border-zinc-300 bg-zinc-50 text-zinc-700',
  READY: 'border-blue-200 bg-blue-50 text-blue-700',
  RUNNING: 'border-violet-200 bg-violet-50 text-violet-700',
  REVIEW: 'border-amber-200 bg-amber-50 text-amber-700',
  ACCEPTED: 'border-emerald-200 bg-emerald-50 text-emerald-700',
  CHANGES_REQUESTED: 'border-orange-200 bg-orange-50 text-orange-700',
  BLOCKED: 'border-rose-200 bg-rose-50 text-rose-700',
  CANCELLED: 'border-zinc-300 bg-zinc-50 text-zinc-500',
}

const runPurposeLabels: Record<RunPurpose, string> = {
	PLANNING: '规划',
	TRIAGE: '评估',
	IMPLEMENTATION: '实现',
	REVISION: '修订',
	REVIEW: '审查',
}

export function TaskDetailsDialog(props: TaskDetailsDialogProps) {
	const { task, tasks, messages, associations, onClose, onOpenTask } = props

  return (
    <Sheet open onOpenChange={(open) => { if (!open) onClose() }}>
      <SheetContent
        className="gap-0 data-[side=right]:w-full data-[side=right]:sm:w-[min(88rem,calc(100vw-3rem))] data-[side=right]:sm:max-w-none"
        showCloseButton={false}
      >
        <SheetHeader className="gap-3 border-b px-5 py-5 sm:px-6">
          <div className="flex items-start justify-between gap-4">
            <div className="min-w-0 space-y-1.5">
              <SheetDescription className="font-mono text-xs">{task.key}</SheetDescription>
              <SheetTitle className="text-xl leading-7">{task.title}</SheetTitle>
            </div>
            <Button variant="ghost" size="icon-sm" type="button" aria-label="关闭" onClick={onClose}><XIcon /></Button>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="outline" className={statusTone[task.status]}>{statusLabels[task.status]}</Badge>
            <Badge variant="secondary" className="font-mono">P{task.priority}</Badge>
			{props.activeRun ? (
				<Badge variant="outline" className="gap-1.5 border-violet-200 bg-violet-50 text-violet-700">
					<LoaderCircleIcon className="size-3.5 animate-spin" />
					{runPurposeLabels[props.activeRun.purpose]} Agent 运行中
				</Badge>
			) : null}
          </div>
			<TaskRequirementsEditor task={task} onUpdate={props.onUpdateDetails} onCancel={props.onCancelTask} onArchive={props.onArchiveTask} />
        </SheetHeader>

		<div className="grid min-h-0 flex-1 grid-cols-[minmax(0,5fr)_minmax(0,7fr)]">
			<section className="flex min-h-0 min-w-0 flex-col border-r" aria-label="需求与讨论">
				<WorkspaceHeading title="需求与讨论" detail="明确边界、验收标准和后续反馈" />
				<div className="min-h-0 flex-1 overflow-y-auto px-5 sm:px-6">
					<DetailSection icon={<TextIcon />} title="Description" value={task.description} />
					<Separator />
					<DetailSection icon={<ListChecksIcon />} title="Acceptance Criteria" value={task.acceptance_criteria} />
					<Separator />
					<SubjectKnowledgePanel kind="tasks" id={task.id} />
					<Separator />
					<TaskClarificationsPanel
						items={props.clarifications}
						error={props.clarificationError}
						answeringID={props.answeringClarificationID}
						onReload={props.onReloadClarifications}
						onAnswer={props.onAnswerClarification}
					/>
					{props.clarifications.length > 0 || props.clarificationError ? <Separator /> : null}
					<RelatedTopics
						topics={props.topics}
						associations={props.topicAssociations}
						loading={props.topicRelationLoading}
						error={props.topicRelationError}
						pendingTopicIDs={props.pendingRelationTopicIDs}
						onAdd={props.onAddTopicRelation}
						onRemove={props.onRemoveTopicRelation}
						onOpenTopic={props.onOpenTopic}
					/>
					<Separator />
					<RelatedTasks
						typed
						tasks={tasks}
						associations={associations}
						excludedTaskID={task.id}
						loading={props.relationLoading}
						error={props.relationError}
						pendingTaskIDs={props.pendingRelationTaskIDs}
						onAdd={props.onAddRelation}
						onRemove={props.onRemoveRelation}
						onOpenTask={onOpenTask}
					/>
					<Separator />
					<DiscussionMessages
						messages={messages}
						feedback={props.feedback}
						tasks={tasks}
						excludedTaskID={task.id}
						loading={props.discussionLoading}
						error={props.discussionError}
						onReload={props.onReloadDiscussion}
						retryingFeedbackID={props.retryingFeedbackID}
						onRetryFeedback={props.onRetryFeedback}
						onOpenTask={onOpenTask}
					/>
				</div>
				<DiscussionComposer
					tasks={tasks}
					excludedTaskID={task.id}
					task={task}
					submitting={props.submitting}
					onSendMessage={props.onSendMessage}
				/>
			</section>

			<section className="flex min-h-0 min-w-0 flex-col bg-muted/10" aria-label="执行现场">
				<WorkspaceHeading title="执行现场" detail="评估、测试、Workspace、Diff 与 Agent Run" />
				<div className="min-h-0 flex-1 overflow-y-auto px-5 sm:px-6">
					<TaskAssessmentPanel task={task} state={props.assessment} loading={props.assessmentLoading} error={props.assessmentError} busy={props.assessmentBusy} onReload={props.onReloadAssessment} onUpdateTitle={props.onUpdateTitle} />
					<Separator />
					<TaskQualityPanel
						quality={props.quality}
						loading={props.qualityLoading}
						error={props.qualityError}
						busy={props.qualityBusy}
						onReload={props.onReloadQuality}
						onCreateEstimate={props.onCreateEstimate}
						onCreateTestCase={props.onCreateTestCase}
						onRecordResult={props.onRecordTestResult}
					/>
					<Separator />
					<TaskWorkspacePanel
						task={task}
						workspace={props.workspace}
						loading={props.workspaceLoading}
						error={props.workspaceError}
						repository={props.repository}
						onUpdateTargetBranch={props.onUpdateTargetBranch}
					/>
					<Separator />
					<Suspense fallback={<p className="py-5 text-sm text-muted-foreground">正在加载修改与验收…</p>}>
						<TaskChangesPanel task={task} hasWorkspace={props.workspace !== null} onTaskUpdated={props.onTaskUpdated} onWorkspaceChanged={props.onWorkspaceChanged} />
					</Suspense>
					<Separator />
					<TaskRunsPanel task={task} onTaskUpdated={props.onTaskUpdated} />
					<Separator />
					<dl className="grid gap-4 py-5 text-sm">
						<DetailItem icon={<Clock3Icon />} label="创建时间" value={formatDate(task.created_at)} />
					</dl>
				</div>
			</section>
		</div>
      </SheetContent>
    </Sheet>
  )
}

function WorkspaceHeading({ title, detail }: { title: string; detail: string }) {
	return <div className="flex items-baseline gap-3 border-b bg-background/80 px-5 py-3 sm:px-6">
		<h3 className="font-medium">{title}</h3>
		<p className="truncate text-xs text-muted-foreground">{detail}</p>
	</div>
}

function DetailSection({ icon, title, value }: { icon: React.ReactNode; title: string; value: string }) {
  return (
    <section className="py-5">
      <h3 className="mb-3 flex items-center gap-2 text-xs font-medium tracking-wide text-muted-foreground uppercase">
        <span className="[&_svg]:size-4">{icon}</span>{title}
      </h3>
      <MarkdownContent content={value} />
    </section>
  )
}

function DetailItem(props: { icon: React.ReactNode; label: string; value: string; mono?: boolean }) {
  return (
    <div className="grid grid-cols-[20px_88px_minmax(0,1fr)] items-start gap-2">
      <span className="text-muted-foreground [&_svg]:size-4">{props.icon}</span>
      <dt className="text-muted-foreground">{props.label}</dt>
      <dd className={`m-0 break-words ${props.mono ? 'font-mono text-xs leading-5' : ''}`}>{props.value}</dd>
    </div>
  )
}

function formatDate(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime())
    ? '未知'
    : new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}
