(() => {
  const eventTypes = [
    'research.created', 'research.started', 'research.paused', 'research.resumed',
    'research.finalizing', 'research.completed', 'research.failed', 'research.cancelled',
    'agent.spawned', 'agent.heartbeat', 'agent.message', 'agent.tool_called', 'agent.failed',
    'task.created', 'task.started', 'task.delegated', 'task.completed', 'task.failed', 'task.recovered',
    'hypothesis.proposed', 'hypothesis.supported', 'hypothesis.refuted', 'hypothesis.verified',
    'paper.discovered', 'paper.downloaded', 'paper.analyzed', 'finding.submitted',
    'experiment.created', 'experiment.started', 'experiment.completed', 'experiment.failed',
    'budget.reserved', 'budget.charged', 'budget.threshold_reached', 'budget.exhausted',
    'human.message', 'human.directive'
  ];
  let source = null;
  let lastEventId = 0;
  let reconnectTimer = null;
  let active = true;
  let terminal = false;
  let visibilityPanel = null;
  let documentListenerAdded = false;

  function setLiveStatus(value) {
    const status = document.getElementById('live-status');
    if (status) status.textContent = `Live updates: ${value}`;
  }

  function eventMatchesFilters(event) {
    const agent = document.getElementById('event-agent-filter')?.value || '';
    const type = document.getElementById('event-type-filter')?.value || '';
    return (!agent || event.actor === agent) && (!type || event.type === type);
  }

  function appendEvent(event) {
    if (!eventMatchesFilters(event)) return;
    const list = document.getElementById('event-list');
    if (!list) return;
    const item = document.createElement('li');
    item.className = 'event-item';
    item.textContent = `${event.timestamp} · ${event.type} · ${event.actor}${event.task_id ? ` · ${event.task_id}` : ''}`;
    list.prepend(item);
    while (list.children.length > 100) list.lastElementChild.remove();
  }

  function handleEvent(event) {
    const sequence = Number.parseInt(event.lastEventId || '0', 10);
    if (sequence > lastEventId) lastEventId = sequence;
    const payload = JSON.parse(event.data);
    if (['research.completed', 'research.failed', 'research.cancelled'].includes(payload.type)) terminal = true;
    if (['research.created', 'research.started', 'research.resumed'].includes(payload.type)) terminal = false;
    appendEvent(payload);
    if (typeof window.refreshResearchGraph === 'function' && !payload.type.startsWith('agent.heartbeat')) {
      window.refreshResearchGraph();
    }
  }

  function connect() {
    if (!active || terminal || source || !document.querySelector('.live-panel')) return;
    reconnectTimer = null;
    setLiveStatus("connecting");
    source = new EventSource(`/runtime/events/stream?after=${lastEventId}`);
    source.onopen = () => setLiveStatus("connected");
    for (const type of eventTypes) source.addEventListener(type, handleEvent);
    source.onerror = () => {
      source?.close();
      source = null;
      setLiveStatus(terminal ? "research complete" : "reconnecting");
      if (active && !terminal && !reconnectTimer) reconnectTimer = setTimeout(connect, 2000);
    };
  }

  function disconnect() {
    setLiveStatus("paused");
    source?.close();
    source = null;
    if (reconnectTimer) clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }

  function setActive(value) {
    active = value && !document.hidden;
    if (active) connect(); else disconnect();
  }

  function initializeVisibility() {
    const panel = document.querySelector('.live-panel');
    if (!panel) return;
    if (panel !== visibilityPanel) {
      visibilityPanel = panel;
      if ('contentVisibility' in document.documentElement.style) {
        panel.addEventListener('contentvisibilityautostatechange', event => setActive(!event.skipped));
      } else if ('IntersectionObserver' in window) {
        const observer = new IntersectionObserver(entries => {
          for (const entry of entries) setActive(entry.isIntersecting);
        }, { rootMargin: '200px' });
        observer.observe(panel);
      }
    }
    if (!documentListenerAdded) {
      documentListenerAdded = true;
      document.addEventListener('visibilitychange', () => setActive(!document.hidden));
    }
    connect();
  }

  function reset() {
    disconnect();
    lastEventId = 0;
    terminal = false;
    document.getElementById('event-list')?.replaceChildren();
    connect();
  }

  window.AutoResearchLive = { connect, disconnect, initializeVisibility, reset, eventTypes };
  document.addEventListener('DOMContentLoaded', initializeVisibility, { once: true });
})();
