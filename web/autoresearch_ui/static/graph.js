(() => {
  let cy = null;
  let latestElements = [];

  function filteredElements() {
    const status = document.getElementById('graph-status-filter')?.value || '';
    const author = (document.getElementById('graph-author-filter')?.value || '').toLowerCase();
    const timeline = Number(document.getElementById('graph-timeline')?.value || 100);
    const timestamps = latestElements.filter(element => element.group === 'nodes')
      .map(element => Date.parse(element.data.created_at)).filter(Number.isFinite).sort((a, b) => a - b);
    const cutoff = timestamps.length ? timestamps[Math.min(timestamps.length - 1, Math.floor((timeline / 100) * timestamps.length))] : Infinity;
    const visible = new Set(latestElements.filter(element => element.group === 'nodes').filter(element => {
      const data = element.data;
      const created = Date.parse(data.created_at);
      return (!status || data.status === status) && (!author || data.author.toLowerCase().includes(author)) && (!Number.isFinite(created) || created <= cutoff);
    }).map(element => element.data.id));
    return latestElements.filter(element => element.group === 'nodes' ? visible.has(element.data.id) : visible.has(element.data.source) && visible.has(element.data.target));
  }

  function render(fit = false) {
    if (!cy) return;
    const viewport = { zoom: cy.zoom(), pan: cy.pan() };
    cy.elements().remove();
    cy.add(filteredElements());
    cy.layout({ name: 'breadthfirst', directed: true, spacingFactor: 1.25, animate: false, fit: true, padding: 35 }).run();
    if (!fit && latestElements.length) { cy.zoom(viewport.zoom); cy.pan(viewport.pan); }
  }

  function initResearchGraph(elements) {
    latestElements = elements || [];
    const container = document.getElementById('research-graph');
    if (!container || typeof cytoscape === 'undefined') return;
    cy = cytoscape({
      container,
      elements: latestElements,
      wheelSensitivity: 0.2,
      style: [
        { selector: 'node', style: { 'label': 'data(label)', 'color': '#e5e7eb', 'font-size': 8, 'text-wrap': 'wrap', 'text-max-width': 95, 'background-color': '#5b8def', 'border-width': 2, 'border-color': '#9bc7ff' } },
        { selector: 'node[type = "hypothesis"]', style: { 'shape': 'diamond', 'background-color': '#f8c471' } },
        { selector: 'node[type = "paper"]', style: { 'shape': 'rectangle', 'background-color': '#9bc7ff' } },
        { selector: 'node[type = "experiment"]', style: { 'shape': 'hexagon', 'background-color': '#b49cff' } },
        { selector: 'node[type = "evidence"]', style: { 'shape': 'ellipse', 'background-color': '#63e6be' } },
        { selector: 'node.status-verified', style: { 'background-color': '#63e6be', 'border-color': '#e5fff4' } },
        { selector: 'node.status-refuted', style: { 'background-color': '#ff9f9f' } },
        { selector: 'edge', style: { 'width': 1.5, 'line-color': '#71809a', 'target-arrow-color': '#a7b2c4', 'target-arrow-shape': 'triangle', 'curve-style': 'bezier', 'label': 'data(label)', 'font-size': 7, 'color': '#a7b2c4' } }
      ]
    });
    cy.on('tap', 'node', event => {
      const details = document.getElementById('node-details');
      if (details) details.textContent = JSON.stringify(event.target.data(), null, 2);
    });
    for (const id of ['graph-status-filter', 'graph-author-filter', 'graph-timeline']) {
      document.getElementById(id)?.addEventListener('input', () => render(false));
      document.getElementById(id)?.addEventListener('change', () => render(false));
    }
    render(true);
  }

  async function refreshResearchGraph() {
    const container = document.getElementById('research-graph');
    if (!container) return;
    try {
      const response = await fetch('/internal/graph/elements', { headers: { 'Accept': 'application/json' } });
      if (!response.ok) {
        latestElements = [];
        render(false);
        return;
      }
      latestElements = await response.json();
      render(false);
    } catch (error) {
      latestElements = [];
      render(false);
      throw error;
    }
  }

  window.initResearchGraph = initResearchGraph;
  window.refreshResearchGraph = refreshResearchGraph;
  window.fitResearchGraph = () => { if (cy) { cy.fit(); cy.center(); } };
})();
