import { lazy, Suspense, useEffect, useMemo, useRef, useState } from 'react'
import { ActivityIcon, AlertCircleIcon, BotIcon, GaugeIcon, ListTodoIcon, MessageSquareTextIcon, RefreshCwIcon } from 'lucide-react'

import { errorMessage } from './api/client'
import { AppHeader } from './components/AppHeader'
import { AgentProfilesPage } from './components/AgentProfilesPage'
import { CreateItemDialog } from './components/CreateItemDialog'
import { KanbanBoard } from './components/KanbanBoard'
import { ReleaseDialog } from './components/ReleaseDialog'
import { RepositoryDialog } from './components/RepositoryDialog'
import { ProgressPage } from './components/ProgressPage'
import { ProjectCapabilitiesPanel } from './components/ProjectCapabilitiesPanel'
import { TaskDetailsDialog } from './components/TaskDetailsDialog'
import { TopicDetailsDialog } from './components/TopicDetailsDialog'
import { TopicList } from './components/TopicList'
import { WorkerSettingsDialog } from './components/WorkerSettingsDialog'
import { ClarificationInboxDialog } from './components/ClarificationsPanel'
import { RunsPage } from './components/RunsPage'
import { Badge } from './components/ui/badge'
import { Button } from './components/ui/button'
import { useTaskBoard } from './features/tasks/useTaskBoard'
import { useDiscussion } from './features/discussion/useDiscussion'
import { useTaskAssociations } from './features/discussion/useTaskAssociations'
import { useTopicAssociations } from './features/discussion/useTopicAssociations'
import { useGitWorkflow } from './features/git/useGitWorkflow'
import { useTaskWorkspace } from './features/git/useTaskWorkspace'
import { useAgentProfiles } from './features/agents/useAgentProfiles'
import { useProjectCapabilities } from './features/agents/useProjectCapabilities'
import { useProjectProgress } from './features/progress/useProjectProgress'
import { useTaskQuality } from './features/quality/useTaskQuality'
import { useTaskAssessment } from './features/assessment/useTaskAssessment'
import { useClarifications } from './features/clarifications/useClarifications'
import { useTopicPlan } from './features/plans/useTopicPlan'
import { useApprovals } from './features/approvals/useApprovals'
import { useBrowserNotifications } from './features/notifications/useBrowserNotifications'
import { useTopicPlanning } from './features/runs/useTopicPlanning'
import { useAgentActivity } from './features/runs/useAgentActivity'
import { useProjectSearch } from './features/search/useProjectSearch'
import type { SearchItem, TaskFeedbackIntent } from './types'

type AppView = 'topics' | 'tasks' | 'runs' | 'progress' | 'agents'

const ProjectSearchDialog = lazy(async () => {
	const module = await import('./components/ProjectSearchDialog')
	return { default: module.ProjectSearchDialog }
})

export default function App() {
  const board = useTaskBoard()
	const [view, setView] = useState<AppView>('topics')
  const [creating, setCreating] = useState(false)
  const [showReleases, setShowReleases] = useState(false)
	const [showRepository, setShowRepository] = useState(false)
	const [showWorkerSettings, setShowWorkerSettings] = useState(false)
  const [showClarifications, setShowClarifications] = useState(false)
	const [showSearch, setShowSearch] = useState(false)
  const [selectedTaskID, setSelectedTaskID] = useState<string | null>(null)
  const [selectedTopicID, setSelectedTopicID] = useState<string | null>(null)
  const selectedTask = useMemo(
    () => board.tasks.find((task) => task.id === selectedTaskID) ?? null,
    [board.tasks, selectedTaskID],
  )
  const selectedTopic = useMemo(
    () => board.topics.find((topic) => topic.id === selectedTopicID) ?? null,
    [board.topics, selectedTopicID],
  )
  const subjectKind = selectedTopicID !== null ? 'topics' : selectedTaskID !== null ? 'tasks' : null
  const subjectID = selectedTopicID ?? selectedTaskID
  const discussion = useDiscussion(subjectKind, subjectID)
  const associations = useTaskAssociations(subjectKind, subjectID, board.tasks)
  const topicAssociations = useTopicAssociations(selectedTaskID, board.topics)
  const git = useGitWorkflow()
  const workspace = useTaskWorkspace(selectedTaskID)
	const quality = useTaskQuality(selectedTaskID)
	const assessment = useTaskAssessment(selectedTaskID)
	const clarifications = useClarifications(selectedTaskID, selectedTopicID, board.project?.workers_enabled ?? false)
	const approvals = useApprovals(board.project?.workers_enabled ?? false)
	const notifications = useBrowserNotifications(board.project?.root, board.project?.workers_enabled ?? false, approvals.items, clarifications.open)
	const progress = useProjectProgress(view === 'progress')
	const agents = useAgentProfiles(view === 'agents')
	const capabilities = useProjectCapabilities(view === 'agents')
	const topicPlan = useTopicPlan(selectedTopicID)
	const topicPlanning = useTopicPlanning(selectedTopic, board.project?.workers_enabled ?? false)
	const agentActivity = useAgentActivity(board.project !== null)
	const projectSearch = useProjectSearch()
	const loadRetrievalEvals = projectSearch.loadEvals
	useEffect(() => {
		if (showSearch) void loadRetrievalEvals()
	}, [loadRetrievalEvals, showSearch])
	const selectedTaskActiveRun = useMemo(
		() => agentActivity.runs.find((run) => run.task_id === selectedTaskID) ?? null,
		[agentActivity.runs, selectedTaskID],
	)
	const observedTaskRun = useRef<{ taskID: string; runID: string } | null>(null)
	const reloadBoard = board.reload
	const reloadDiscussion = discussion.reload
	const reloadTopicPlan = topicPlan.reload
	const planningCompletionKey = topicPlanning.run && ['NEEDS_INPUT', 'SUCCEEDED', 'FAILED', 'CANCELLED', 'TIMED_OUT', 'LOST'].includes(topicPlanning.run.status)
		? `${topicPlanning.run.id}:${topicPlanning.run.status}:${topicPlanning.run.updated_at}`
		: null

	useEffect(() => {
		if (planningCompletionKey === null) return
		reloadDiscussion()
		reloadTopicPlan()
		reloadBoard()
	}, [planningCompletionKey, reloadBoard, reloadDiscussion, reloadTopicPlan])

	useEffect(() => {
		if (selectedTaskID === null) {
			observedTaskRun.current = null
			return
		}
		if (observedTaskRun.current?.taskID !== selectedTaskID) observedTaskRun.current = null
		if (selectedTaskActiveRun !== null) {
			observedTaskRun.current = { taskID: selectedTaskID, runID: selectedTaskActiveRun.id }
			return
		}
		if (observedTaskRun.current !== null) {
			observedTaskRun.current = null
			reloadDiscussion()
			reloadBoard()
		}
	}, [reloadBoard, reloadDiscussion, selectedTaskActiveRun, selectedTaskID])

  function openTask(taskID: string) {
    setSelectedTopicID(null)
    setSelectedTaskID(taskID)
    setView('tasks')
  }

  function openTopic(topicID: string) {
    setSelectedTaskID(null)
    setSelectedTopicID(topicID)
    setView('topics')
  }

  async function sendMessage(content: string, linkedTaskIDs: string[], intent?: TaskFeedbackIntent) {
    const result = await discussion.sendMessage(content, linkedTaskIDs, intent, selectedTask?.version)
    associations.includeTaskIDs(linkedTaskIDs)
		if (selectedTopicID !== null) {
			board.reload()
			topicPlanning.reload()
		}
		if (selectedTaskID !== null) {
			if (result?.task) board.updateTask(result.task)
			if (result?.follow_up_task) board.reload()
			agentActivity.reload()
		}
  }

  useEffect(() => {
    function handleShortcut(event: KeyboardEvent) {
      if (
        event.defaultPrevented ||
        event.repeat ||
        isEditableTarget(event.target)
      ) return
		const key = event.key.toLowerCase()
		const modalOpen = creating || showSearch || selectedTaskID !== null || selectedTopicID !== null ||
			showReleases || showRepository || showWorkerSettings || showClarifications
		if (key === '/' && !event.metaKey && !event.ctrlKey && !event.altKey && !event.shiftKey && !modalOpen) {
			event.preventDefault()
			setShowSearch(true)
			return
		}
		if (key !== 'n' || event.metaKey || event.ctrlKey || event.altKey || event.shiftKey || modalOpen) return
      event.preventDefault()
      setCreating(true)
    }
    document.addEventListener('keydown', handleShortcut)
    return () => document.removeEventListener('keydown', handleShortcut)
  }, [creating, selectedTaskID, selectedTopicID, showClarifications, showReleases, showRepository, showSearch, showWorkerSettings])

	function openSearchItem(item: SearchItem) {
		setShowSearch(false)
		projectSearch.reset()
		if (item.subject_kind === 'TOPIC') openTopic(item.subject_id)
		else openTask(item.subject_id)
	}

  return (
    <div className="min-h-svh bg-background">
      <AppHeader
        project={board.project}
        topicCount={board.topics.length}
        taskCount={board.tasks.length}
        repository={git.repository}
        latestRelease={git.releases[0] ?? null}
        onOpenReleases={() => setShowReleases(true)}
		onOpenRepository={() => setShowRepository(true)}
		onToggleWorkers={() => {
			if (!board.project) return
			void board.updateWorkers(!board.project.workers_enabled, board.project.max_workers)
		}}
		onConfigureWorkers={() => setShowWorkerSettings(true)}
		workersPending={board.updatingWorkers}
		activeRuns={agentActivity.runs}
		agentActivityError={agentActivity.error}
		onOpenRuns={() => {
			setSelectedTaskID(null)
			setSelectedTopicID(null)
			setView('runs')
		}}
		onReloadAgentActivity={agentActivity.reload}
		attentionCount={clarifications.open.length + approvals.items.length}
		onOpenClarifications={() => setShowClarifications(true)}
		notificationLabel={notifications.label}
		notificationsEnabled={notifications.enabled}
		notificationsSupported={notifications.supported}
		notificationsBlocked={notifications.blocked}
		onToggleNotifications={() => { void notifications.toggle() }}
		onOpenSearch={() => setShowSearch(true)}
        onCreate={() => setCreating(true)}
      />
      <main className="min-w-0 py-6">
        <div className="flex flex-col gap-4 px-4 pb-5 sm:px-6 lg:flex-row lg:items-end lg:justify-between lg:px-8">
          <div>
            <p className="mb-1 text-xs font-medium tracking-wide text-muted-foreground uppercase">Agent workflow</p>
            <h2 className="font-heading text-2xl font-semibold tracking-tight">
			{viewTitle(view)}
            </h2>
          </div>
          <ViewTabs view={view} topicCount={board.topics.length} taskCount={board.tasks.length} onChange={setView} />
        </div>
        {board.error ? (
          <div className="px-4 pb-4 sm:px-6 lg:px-8">
            <div className="flex items-center gap-3 rounded-xl border border-destructive/20 bg-destructive/5 px-4 py-3 text-sm text-destructive" role="alert">
              <AlertCircleIcon className="size-4 shrink-0" />
              <span className="min-w-0 flex-1">{errorMessage(board.error)}</span>
              <Button variant="ghost" size="sm" type="button" onClick={board.reload}>
                <RefreshCwIcon />
                重新加载
              </Button>
            </div>
          </div>
        ) : null}
		{git.error ? (
			<div className="px-4 pb-4 sm:px-6 lg:px-8"><div className="flex items-center gap-3 rounded-xl border border-destructive/20 bg-destructive/5 px-4 py-3 text-sm text-destructive" role="alert"><AlertCircleIcon className="size-4 shrink-0" /><span className="min-w-0 flex-1">Git：{errorMessage(git.error)}</span><Button variant="ghost" size="sm" type="button" onClick={git.reload}><RefreshCwIcon />重新读取</Button></div></div>
		) : null}
		{view === 'topics' ? (
          <TopicList topics={board.topics} loading={board.loading} onOpenTopic={setSelectedTopicID} />
		) : view === 'tasks' ? (
          <KanbanBoard tasks={board.tasks} loading={board.loading} onOpenTask={setSelectedTaskID} />
		) : view === 'runs' ? (
			<RunsPage enabled tasks={board.tasks} onOpenTask={openTask} />
		) : view === 'progress' ? (
			<ProgressPage {...progress} onReload={progress.reload} />
		) : (
			<><ProjectCapabilitiesPanel catalog={capabilities.catalog} loading={capabilities.loading} adding={capabilities.adding} error={capabilities.error} onReload={capabilities.reload} onAddSkill={capabilities.addSkill} onRefreshSkill={capabilities.refreshSkill} onAddMCPServer={capabilities.addMCPServer} /><AgentProfilesPage profiles={agents.profiles} capabilities={capabilities.catalog} loading={agents.loading} error={agents.error} saving={agents.saving} onReload={agents.reload} onSave={agents.save} onConfigureDefaults={agents.configureDefaults} /></>
		)}
      </main>
		{showSearch ? <Suspense fallback={null}><ProjectSearchDialog
			items={projectSearch.items} loading={projectSearch.loading} error={projectSearch.error}
			nextCursor={projectSearch.nextCursor}
			evalCases={projectSearch.evalCases} evalRuns={projectSearch.evalRuns}
			evalLoading={projectSearch.evalLoading} evalError={projectSearch.evalError}
			onClose={() => { setShowSearch(false); projectSearch.reset() }}
			onSearch={(input) => { void projectSearch.search(input) }}
			onLoadMore={() => { void projectSearch.loadMore() }} onOpenItem={openSearchItem}
			onAddEvalResult={(input) => { void projectSearch.addEvalResult(input) }}
			onRemoveEvalResult={(caseID, documentID) => { void projectSearch.removeEvalResult(caseID, documentID) }}
			onRunEval={(k) => { void projectSearch.runEval(k) }}
		/></Suspense> : null}
      {creating ? (
        <CreateItemDialog
			repository={git.repository}
          onClose={() => setCreating(false)}
          onCreateTopic={async (input) => {
			await board.createTopic(input)
            setView('topics')
          }}
          onCreateTask={async (input) => {
            await board.createTask(input)
            setView('tasks')
          }}
        />
      ) : null}
      {selectedTask ? (
        <TaskDetailsDialog
          key={selectedTask.id}
          task={selectedTask}
		  activeRun={selectedTaskActiveRun}
          tasks={board.tasks}
          topics={board.topics}
          messages={discussion.messages}
		  feedback={discussion.feedback}
          associations={associations.associations}
          topicAssociations={topicAssociations.associations}
          discussionLoading={discussion.loading}
          relationLoading={associations.loading}
          submitting={discussion.submitting}
		  retryingFeedbackID={discussion.retryingFeedbackID}
          discussionError={discussion.error}
          relationError={associations.error}
          pendingRelationTaskIDs={associations.pendingTaskIDs}
          topicRelationLoading={topicAssociations.loading}
          topicRelationError={topicAssociations.error}
          pendingRelationTopicIDs={topicAssociations.pendingTopicIDs}
          workspace={workspace.workspace}
          workspaceLoading={workspace.loading}
          workspaceError={workspace.error}
		quality={quality.quality}
		qualityLoading={quality.loading}
		qualityError={quality.error}
		qualityBusy={quality.busy}
		assessment={assessment.assessment}
		assessmentLoading={assessment.loading}
		assessmentError={assessment.error}
		assessmentBusy={assessment.busy}
		clarifications={clarifications.history}
		clarificationError={clarifications.error}
		answeringClarificationID={clarifications.answeringID}
			repository={git.repository}
          onClose={() => setSelectedTaskID(null)}
          onReloadDiscussion={discussion.reload}
		  onRetryFeedback={discussion.retryFeedback}
          onSendMessage={sendMessage}
          onAddRelation={associations.add}
          onRemoveRelation={associations.remove}
          onOpenTask={openTask}
          onAddTopicRelation={topicAssociations.add}
          onRemoveTopicRelation={topicAssociations.remove}
          onOpenTopic={openTopic}
			onTaskUpdated={board.updateTask}
		onReloadQuality={quality.reload}
		onCreateEstimate={quality.createEstimate}
		onCreateTestCase={quality.createTestCase}
		onRecordTestResult={quality.recordResult}
		onReloadAssessment={assessment.reload}
		onReloadClarifications={clarifications.reload}
		onAnswerClarification={async (item, input) => {
			const updated = await clarifications.answer(item, input)
			if (updated.task) board.updateTask(updated.task)
		}}
		onUpdateTitle={async (title) => {
			const updated = await assessment.updateTitle(selectedTask, title)
			board.updateTask(updated)
		}}
		onUpdateTargetBranch={async (targetBranch) => {
			await board.updateTargetBranch(selectedTask, targetBranch)
		}}
		onUpdateDetails={async (input) => {
			await board.updateTaskDetails(selectedTask, input)
		}}
		onCancelTask={async () => {
			await board.cancelTask(selectedTask)
		}}
		onArchiveTask={async () => {
			await board.archiveTask(selectedTask)
			setSelectedTaskID(null)
		}}
			onWorkspaceChanged={() => {
			workspace.reload()
			git.reload()
		}}
        />
      ) : null}
      {selectedTopic ? (
        <TopicDetailsDialog
          key={selectedTopic.id}
          topic={selectedTopic}
          tasks={board.tasks}
          messages={discussion.messages}
          associations={associations.associations}
          discussionLoading={discussion.loading}
          relationLoading={associations.loading}
          submitting={discussion.submitting}
          discussionError={discussion.error}
          relationError={associations.error}
		  pendingRelationTaskIDs={associations.pendingTaskIDs}
		  clarifications={clarifications.history}
		  clarificationError={clarifications.error}
		  answeringClarificationID={clarifications.answeringID}
          onClose={() => setSelectedTopicID(null)}
          onReloadDiscussion={discussion.reload}
          onSendMessage={sendMessage}
          onAddRelation={associations.add}
          onRemoveRelation={associations.remove}
		  onOpenTask={openTask}
		  onReloadClarifications={clarifications.reload}
		  onAnswerClarification={async (item, input) => {
			  const updated = await clarifications.answer(item, input)
			  if (updated.topic) board.updateTopic(updated.topic)
		  }}
			plan={topicPlan.plan}
			planLoading={topicPlan.loading}
			planSubmitting={topicPlan.submitting}
			planError={topicPlan.error}
			planningRun={topicPlanning.run}
			planningLoading={topicPlanning.loading}
			planningError={topicPlanning.error}
			requestingPlanning={topicPlanning.requesting}
			workersEnabled={board.project?.workers_enabled ?? false}
			onReloadPlan={() => topicPlan.reload()}
			onRequestPlanning={async () => {
				const updated = await topicPlanning.requestPlanning()
				board.updateTopic(updated)
			}}
			onSubmitPlan={async (input) => {
				await topicPlan.submit(selectedTopic, input)
				board.reload()
			}}
			onRejectPlan={async (comment) => {
				await topicPlan.reject(selectedTopic, comment)
				board.reload()
			}}
			onApprovePlan={async (comment) => {
				await topicPlan.approve(selectedTopic, comment)
				board.reload()
				associations.reload()
			}}
        />
      ) : null}
      {showReleases && git.repository ? (
        <ReleaseDialog
          repository={git.repository}
          releases={git.releases}
          submitting={git.submitting}
          onClose={() => setShowReleases(false)}
          onCreate={git.createRelease}
        />
      ) : null}
		{showRepository && git.repository ? (
			<RepositoryDialog repository={git.repository} onClose={() => setShowRepository(false)} />
		) : null}
		{showWorkerSettings && board.project ? (
			<WorkerSettingsDialog
				project={board.project}
				onClose={() => setShowWorkerSettings(false)}
				onSave={board.updateWorkers}
			/>
		) : null}
		{showClarifications ? (
			<ClarificationInboxDialog
				items={clarifications.open}
				approvals={approvals.items}
				tasks={board.tasks}
				topics={board.topics}
				answeringID={clarifications.answeringID}
				decidingApprovalID={approvals.decidingID}
				onClose={() => setShowClarifications(false)}
				onOpenTask={(taskID) => { setShowClarifications(false); openTask(taskID) }}
				onOpenTopic={(topicID) => { setShowClarifications(false); openTopic(topicID) }}
				onAnswer={async (item, input) => {
					const updated = await clarifications.answer(item, input)
					if (updated.task) board.updateTask(updated.task)
					if (updated.topic) board.updateTopic(updated.topic)
				}}
				onDecideApproval={approvals.decide}
			/>
		) : null}
    </div>
  )
}

function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  return target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName)
}

function ViewTabs({
  view,
  topicCount,
  taskCount,
  onChange,
}: {
	view: AppView
  topicCount: number
  taskCount: number
	onChange: (view: AppView) => void
}) {
  return (
    <div className="flex w-fit rounded-lg border bg-muted/40 p-1" role="tablist" aria-label="工作类型">
      <Button
        variant={view === 'topics' ? 'secondary' : 'ghost'}
        size="sm"
        type="button"
        role="tab"
        aria-selected={view === 'topics'}
        onClick={() => onChange('topics')}
      >
        <MessageSquareTextIcon />议题 Topics <Badge variant="outline">{topicCount}</Badge>
      </Button>
      <Button
        variant={view === 'tasks' ? 'secondary' : 'ghost'}
        size="sm"
        type="button"
        role="tab"
        aria-selected={view === 'tasks'}
        onClick={() => onChange('tasks')}
      >
        <ListTodoIcon />任务 Tasks <Badge variant="outline">{taskCount}</Badge>
      </Button>
		<Button variant={view === 'runs' ? 'secondary' : 'ghost'} size="sm" type="button" role="tab" aria-selected={view === 'runs'} onClick={() => onChange('runs')}>
			<ActivityIcon />Runs
		</Button>
		<Button variant={view === 'progress' ? 'secondary' : 'ghost'} size="sm" type="button" role="tab" aria-selected={view === 'progress'} onClick={() => onChange('progress')}>
			<GaugeIcon />整体进度
		</Button>
		<Button variant={view === 'agents' ? 'secondary' : 'ghost'} size="sm" type="button" role="tab" aria-selected={view === 'agents'} onClick={() => onChange('agents')}>
			<BotIcon />Agents
		</Button>
    </div>
  )
}

function viewTitle(view: AppView): string {
	return ({ topics: '议题', tasks: '任务看板', runs: '执行历史', progress: '整体进度', agents: 'Agent 配置' })[view]
}
