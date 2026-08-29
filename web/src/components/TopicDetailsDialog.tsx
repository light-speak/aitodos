import type { ComponentProps } from 'react'
import { LoaderCircleIcon, TextIcon, XIcon } from 'lucide-react'

import type { DiscussionMessage, Task, TaskAssociation, TaskFeedbackIntent, Topic, TopicStatus } from '../types'
import { DiscussionComposer, DiscussionMessages } from './DiscussionPanel'
import { MarkdownContent } from './MarkdownContent'
import { PlanPanel } from './PlanPanel'
import { RelatedTasks } from './RelatedTasks'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Separator } from './ui/separator'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from './ui/sheet'

interface TopicDetailsDialogProps {
  topic: Topic
  tasks: Task[]
  messages: DiscussionMessage[]
  associations: TaskAssociation[]
  discussionLoading: boolean
  relationLoading: boolean
  submitting: boolean
  discussionError: unknown
  relationError: unknown
  pendingRelationTaskIDs: Set<string>
  onClose: () => void
  onReloadDiscussion: () => void
  onSendMessage: (content: string, linkedTaskIDs: string[], intent?: TaskFeedbackIntent) => Promise<void>
  onAddRelation: (taskID: string) => Promise<void>
  onRemoveRelation: (taskID: string) => Promise<void>
  onOpenTask: (taskID: string) => void
	plan: ComponentProps<typeof PlanPanel>['plan']
	planLoading: boolean
	planSubmitting: boolean
	planError: unknown
	planningRun: ComponentProps<typeof PlanPanel>['planningRun']
	planningLoading: boolean
	planningError: unknown
	requestingPlanning: boolean
	workersEnabled: boolean
	onReloadPlan: () => void
	onRequestPlanning: ComponentProps<typeof PlanPanel>['onRequestPlanning']
	onSubmitPlan: ComponentProps<typeof PlanPanel>['onSubmit']
	onRejectPlan: ComponentProps<typeof PlanPanel>['onReject']
	onApprovePlan: ComponentProps<typeof PlanPanel>['onApprove']
}

const statusLabels: Record<TopicStatus, { phase: string; detail?: string }> = {
  OPEN: { phase: '讨论中' },
  NEEDS_CLARIFICATION: { phase: '讨论中', detail: '待澄清' },
  PLAN_REVIEW: { phase: '待确认', detail: '方案' },
  PLANNED: { phase: '已完成', detail: '已规划' },
  CLOSED: { phase: '已完成' },
}

export function TopicDetailsDialog(props: TopicDetailsDialogProps) {
  const { topic, tasks, messages, associations, onClose, onOpenTask } = props
  return (
    <Sheet open onOpenChange={(open) => { if (!open) onClose() }}>
      <SheetContent
        className="gap-0 data-[side=right]:w-full data-[side=right]:sm:w-[min(88rem,calc(100vw-3rem))] data-[side=right]:sm:max-w-none"
        showCloseButton={false}
      >
        <SheetHeader className="gap-3 border-b px-5 py-5 sm:px-6">
          <div className="flex items-start justify-between gap-4">
            <div className="min-w-0 space-y-1.5">
              <SheetDescription className="font-mono text-xs">{topic.key}</SheetDescription>
              <SheetTitle className="text-xl leading-7">{topic.title}</SheetTitle>
            </div>
            <Button variant="ghost" size="icon-sm" type="button" aria-label="关闭" onClick={onClose}><XIcon /></Button>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="secondary">{statusLabels[topic.status].phase}</Badge>
            {statusLabels[topic.status].detail ? <Badge variant="outline">{statusLabels[topic.status].detail}</Badge> : null}
			{props.planningRun && ['CLAIMED', 'STARTING', 'RUNNING', 'FINALIZING'].includes(props.planningRun.status) ? <Badge variant="outline" className="gap-1.5 border-violet-200 bg-violet-50 text-violet-700"><LoaderCircleIcon className="size-3.5 animate-spin" />Agent 分析中</Badge> : null}
          </div>
        </SheetHeader>

		<div className="grid min-h-0 flex-1 grid-cols-[minmax(0,5fr)_minmax(0,7fr)]">
			<section className="flex min-h-0 min-w-0 flex-col border-r" aria-label="讨论区">
				<WorkspaceHeading title="讨论区" detail="补充背景、约束和结论，Agent 会自动跟进" />
				<div className="min-h-0 flex-1 overflow-y-auto px-5 sm:px-6">
					<section className="py-5">
						<h3 className="mb-3 flex items-center gap-2 text-xs font-medium text-muted-foreground">
							<TextIcon className="size-4" />议题描述
						</h3>
						<MarkdownContent content={topic.description} emptyText="未填写描述" />
					</section>
					<Separator />
					<DiscussionMessages
						messages={messages}
						tasks={tasks}
						loading={props.discussionLoading}
						error={props.discussionError}
						onReload={props.onReloadDiscussion}
						onOpenTask={onOpenTask}
					/>
				</div>
				<DiscussionComposer
					tasks={tasks}
					submitting={props.submitting}
					onSendMessage={props.onSendMessage}
				/>
			</section>

			<section className="min-h-0 min-w-0 bg-muted/10" aria-label="执行区">
				<div className="flex h-full min-h-0 flex-col">
					<WorkspaceHeading title="执行区" detail="确认 Agent 方案，再创建可执行 Task" />
					<div className="min-h-0 flex-1 overflow-y-auto px-5 sm:px-6">
						<PlanPanel
							topic={topic}
							plan={props.plan}
							loading={props.planLoading}
							submitting={props.planSubmitting}
							error={props.planError}
							planningRun={props.planningRun}
							planningLoading={props.planningLoading}
							planningError={props.planningError}
							requestingPlanning={props.requestingPlanning}
							workersEnabled={props.workersEnabled}
							onReload={props.onReloadPlan}
							onRequestPlanning={props.onRequestPlanning}
							onSubmit={props.onSubmitPlan}
							onReject={props.onRejectPlan}
							onApprove={props.onApprovePlan}
						/>
						<Separator />
						<RelatedTasks
							tasks={tasks}
							associations={associations}
							loading={props.relationLoading}
							error={props.relationError}
							pendingTaskIDs={props.pendingRelationTaskIDs}
							onAdd={props.onAddRelation}
							onRemove={props.onRemoveRelation}
							onOpenTask={onOpenTask}
						/>
					</div>
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
