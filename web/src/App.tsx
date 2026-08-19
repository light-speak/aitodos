import { useEffect, useMemo, useState } from 'react'
import { AlertCircleIcon, ListTodoIcon, MessageSquareTextIcon, RefreshCwIcon } from 'lucide-react'

import { errorMessage } from './api/client'
import { AppHeader } from './components/AppHeader'
import { CreateItemDialog } from './components/CreateItemDialog'
import { KanbanBoard } from './components/KanbanBoard'
import { ReleaseDialog } from './components/ReleaseDialog'
import { TaskDetailsDialog } from './components/TaskDetailsDialog'
import { TopicDetailsDialog } from './components/TopicDetailsDialog'
import { TopicList } from './components/TopicList'
import { Badge } from './components/ui/badge'
import { Button } from './components/ui/button'
import { useTaskBoard } from './features/tasks/useTaskBoard'
import { useDiscussion } from './features/discussion/useDiscussion'
import { useTaskAssociations } from './features/discussion/useTaskAssociations'
import { useTopicAssociations } from './features/discussion/useTopicAssociations'
import { useGitWorkflow } from './features/git/useGitWorkflow'
import { useTaskWorkspace } from './features/git/useTaskWorkspace'

export default function App() {
  const board = useTaskBoard()
  const [view, setView] = useState<'topics' | 'tasks'>('topics')
  const [creating, setCreating] = useState(false)
  const [showReleases, setShowReleases] = useState(false)
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

  async function sendMessage(content: string, linkedTaskIDs: string[]) {
    await discussion.sendMessage(content, linkedTaskIDs)
    associations.includeTaskIDs(linkedTaskIDs)
  }

  useEffect(() => {
    function handleShortcut(event: KeyboardEvent) {
      if (
        event.defaultPrevented ||
        event.repeat ||
        event.key.toLowerCase() !== 'n' ||
        event.metaKey ||
        event.ctrlKey ||
        event.altKey ||
        event.shiftKey ||
        creating ||
        selectedTaskID !== null ||
        selectedTopicID !== null ||
        isEditableTarget(event.target)
      ) return
      event.preventDefault()
      setCreating(true)
    }
    document.addEventListener('keydown', handleShortcut)
    return () => document.removeEventListener('keydown', handleShortcut)
  }, [creating, selectedTaskID, selectedTopicID])

  return (
    <div className="min-h-svh bg-background">
      <AppHeader
        project={board.project}
        topicCount={board.topics.length}
        taskCount={board.tasks.length}
        repository={git.repository}
        latestRelease={git.releases[0] ?? null}
        onOpenReleases={() => setShowReleases(true)}
        onCreate={() => setCreating(true)}
      />
      <main className="min-w-0 py-6">
        <div className="flex flex-col gap-4 px-4 pb-5 sm:px-6 lg:flex-row lg:items-end lg:justify-between lg:px-8">
          <div>
            <p className="mb-1 text-xs font-medium tracking-wide text-muted-foreground uppercase">Agent workflow</p>
            <h2 className="font-heading text-2xl font-semibold tracking-tight">
              {view === 'topics' ? '议题' : '任务看板'}
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
        {view === 'topics' ? (
          <TopicList topics={board.topics} loading={board.loading} onOpenTopic={setSelectedTopicID} />
        ) : (
          <KanbanBoard tasks={board.tasks} loading={board.loading} onOpenTask={setSelectedTaskID} />
        )}
      </main>
      {creating ? (
        <CreateItemDialog
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
          tasks={board.tasks}
          topics={board.topics}
          messages={discussion.messages}
          associations={associations.associations}
          topicAssociations={topicAssociations.associations}
          pending={board.pendingTaskIDs.has(selectedTask.id)}
          discussionLoading={discussion.loading}
          relationLoading={associations.loading}
          submitting={discussion.submitting}
          discussionError={discussion.error}
          relationError={associations.error}
          pendingRelationTaskIDs={associations.pendingTaskIDs}
          topicRelationLoading={topicAssociations.loading}
          topicRelationError={topicAssociations.error}
          pendingRelationTopicIDs={topicAssociations.pendingTopicIDs}
          workspace={workspace.workspace}
          workspaceLoading={workspace.loading}
          workspaceCreating={workspace.creating}
          workspaceError={workspace.error}
          onClose={() => setSelectedTaskID(null)}
          onQueue={board.queueTask}
          onReloadDiscussion={discussion.reload}
          onSendMessage={sendMessage}
          onAddRelation={associations.add}
          onRemoveRelation={associations.remove}
          onOpenTask={openTask}
          onAddTopicRelation={topicAssociations.add}
          onRemoveTopicRelation={topicAssociations.remove}
          onOpenTopic={openTopic}
          onCreateWorkspace={async () => {
            await workspace.create()
            board.reload()
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
          onClose={() => setSelectedTopicID(null)}
          onReloadDiscussion={discussion.reload}
          onSendMessage={sendMessage}
          onAddRelation={associations.add}
          onRemoveRelation={associations.remove}
          onOpenTask={openTask}
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
  view: 'topics' | 'tasks'
  topicCount: number
  taskCount: number
  onChange: (view: 'topics' | 'tasks') => void
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
    </div>
  )
}
