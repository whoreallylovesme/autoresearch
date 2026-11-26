package graph

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

var ErrVerificationRequired = errors.New("objective verification evidence is required")

type NodeType string

const (
	NodeObjective      NodeType = "research_objective"
	NodeDirection      NodeType = "research_direction"
	NodeHypothesis     NodeType = "hypothesis"
	NodeClaim          NodeType = "scientific_claim"
	NodePaper          NodeType = "paper"
	NodeEvidence       NodeType = "evidence"
	NodeExperiment     NodeType = "experiment"
	NodeBenchmark      NodeType = "benchmark"
	NodeCounterexample NodeType = "counterexample"
	NodeProof          NodeType = "proof"
	NodeRefutation     NodeType = "refutation"
	NodeArtifact       NodeType = "artifact"
	NodeConclusion     NodeType = "conclusion"
)

type EdgeType string

const (
	EdgeDependsOn    EdgeType = "depends_on"
	EdgeSupports     EdgeType = "supports"
	EdgeContradicts  EdgeType = "contradicts"
	EdgeDerivedFrom  EdgeType = "derived_from"
	EdgeInvestigates EdgeType = "investigates"
	EdgeVerifies     EdgeType = "verifies"
	EdgeRefutes      EdgeType = "refutes"
	EdgeImplements   EdgeType = "implements"
	EdgeCites        EdgeType = "cites"
)

const (
	StatusProposed      = "proposed"
	StatusInvestigating = "investigating"
	StatusSupported     = "supported"
	StatusVerified      = "verified"
	StatusRefuted       = "refuted"
	StatusInconclusive  = "inconclusive"
	StatusAbandoned     = "abandoned"
)

type Node struct {
	ID           string    `json:"id"`
	RunID        string    `json:"run_id"`
	Type         NodeType  `json:"type"`
	Author       string    `json:"author"`
	CreatedAt    time.Time `json:"created_at"`
	Status       string    `json:"status"`
	Description  string    `json:"description"`
	RelatedTasks []string  `json:"related_tasks,omitempty"`
	Artifacts    []string  `json:"artifacts,omitempty"`
	EvidenceRefs []string  `json:"evidence_refs,omitempty"`
	Revision     int       `json:"revision"`
}

type Revision struct {
	NodeID    string    `json:"node_id"`
	Revision  int       `json:"revision"`
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"created_at"`
	Node      Node      `json:"node"`
}

type Edge struct {
	ID        string    `json:"id"`
	RunID     string    `json:"run_id"`
	FromNode  string    `json:"from_node"`
	ToNode    string    `json:"to_node"`
	Type      EdgeType  `json:"type"`
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"created_at"`
}

type Verification struct {
	ID          string   `json:"id"`
	Verifier    string   `json:"verifier"`
	Passed      bool     `json:"passed"`
	Command     string   `json:"command"`
	ExitCode    int      `json:"exit_code"`
	ArtifactIDs []string `json:"artifact_ids"`
}

type Snapshot struct {
	Nodes     []Node     `json:"nodes"`
	Edges     []Edge     `json:"edges"`
	Revisions []Revision `json:"revisions"`
}

type Graph struct {
	mu          sync.Mutex
	nodes       map[string]Node
	edges       map[string]Edge
	revisions   map[string][]Revision
	persistence func(Snapshot) error
}

func New() *Graph {
	return &Graph{nodes: map[string]Node{}, edges: map[string]Edge{}, revisions: map[string][]Revision{}}
}

func (g *Graph) AddNode(node Node) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if node.ID == "" || node.RunID == "" || node.Author == "" || node.Description == "" || !validNodeType(node.Type) {
		return errors.New("node ID, run, valid type, author, and description are required")
	}
	if _, exists := g.nodes[node.ID]; exists {
		return fmt.Errorf("node %s already exists", node.ID)
	}
	if node.CreatedAt.IsZero() {
		node.CreatedAt = time.Now().UTC()
	}
	node.Revision = 1
	g.nodes[node.ID] = cloneNode(node)
	g.revisions[node.ID] = append(g.revisions[node.ID], Revision{NodeID: node.ID, Revision: 1, Author: node.Author, CreatedAt: node.CreatedAt, Node: cloneNode(node)})
	return g.persistLocked()
}

func (g *Graph) ReviseNode(node Node) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	current, ok := g.nodes[node.ID]
	if !ok || current.RunID != node.RunID || current.Type != node.Type {
		return errors.New("node revision must preserve identity, run, and type")
	}
	if node.Author == "" || node.Description == "" {
		return errors.New("node revision requires author and description")
	}
	node.CreatedAt = current.CreatedAt
	node.Revision = current.Revision + 1
	g.nodes[node.ID] = cloneNode(node)
	g.revisions[node.ID] = append(g.revisions[node.ID], Revision{NodeID: node.ID, Revision: node.Revision, Author: node.Author, CreatedAt: time.Now().UTC(), Node: cloneNode(node)})
	return g.persistLocked()
}

func (g *Graph) AddEdge(edge Edge) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if edge.ID == "" || edge.RunID == "" || edge.Author == "" || edge.FromNode == edge.ToNode || !validEdgeType(edge.Type) {
		return errors.New("valid edge ID, run, endpoints, type, and author are required")
	}
	from, fromOK := g.nodes[edge.FromNode]
	to, toOK := g.nodes[edge.ToNode]
	if !fromOK || !toOK || from.RunID != edge.RunID || to.RunID != edge.RunID {
		return errors.New("edge endpoints must exist in the same research run")
	}
	if _, exists := g.edges[edge.ID]; exists {
		return fmt.Errorf("edge %s already exists", edge.ID)
	}
	if edge.CreatedAt.IsZero() {
		edge.CreatedAt = time.Now().UTC()
	}
	g.edges[edge.ID] = edge
	return g.persistLocked()
}

func (g *Graph) TransitionHypothesis(id, status string, evidence []Verification) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	node, ok := g.nodes[id]
	if !ok || node.Type != NodeHypothesis {
		return errors.New("hypothesis not found")
	}
	if !allowedTransition(node.Status, status) {
		return fmt.Errorf("invalid hypothesis transition %s -> %s", node.Status, status)
	}
	if status == StatusVerified {
		if node.Status != StatusSupported || !validVerification(evidence) {
			return ErrVerificationRequired
		}
		for _, verification := range evidence {
			node.EvidenceRefs = append(node.EvidenceRefs, verification.ID)
			node.Artifacts = append(node.Artifacts, verification.ArtifactIDs...)
		}
	}
	node.Status = status
	node.Revision++
	g.nodes[id] = cloneNode(node)
	g.revisions[id] = append(g.revisions[id], Revision{NodeID: id, Revision: node.Revision, Author: node.Author, CreatedAt: time.Now().UTC(), Node: cloneNode(node)})
	return g.persistLocked()
}

func (g *Graph) Node(id string) (Node, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	node, ok := g.nodes[id]
	return cloneNode(node), ok
}

func (g *Graph) Nodes() []Node {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.nodesLocked()
}

func (g *Graph) Edges() []Edge {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.edgesLocked()
}

func (g *Graph) Revisions(nodeID string) []Revision {
	g.mu.Lock()
	defer g.mu.Unlock()
	revisions := cloneRevisions(g.revisions[nodeID])
	sort.Slice(revisions, func(i, j int) bool { return revisions[i].Revision < revisions[j].Revision })
	return revisions
}

func (g *Graph) Snapshot() Snapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.snapshotLocked()
}

func (g *Graph) SetPersistence(persistence func(Snapshot) error) {
	g.mu.Lock()
	g.persistence = persistence
	g.mu.Unlock()
}

func (g *Graph) Restore(snapshot Snapshot) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	nodes := map[string]Node{}
	edges := map[string]Edge{}
	revisions := map[string][]Revision{}
	for _, node := range snapshot.Nodes {
		if node.ID == "" || node.Revision < 1 {
			return errors.New("invalid graph snapshot node")
		}
		nodes[node.ID] = cloneNode(node)
	}
	for _, revision := range snapshot.Revisions {
		if _, ok := nodes[revision.NodeID]; !ok {
			return errors.New("revision references missing node")
		}
		revision.Node = cloneNode(revision.Node)
		revisions[revision.NodeID] = append(revisions[revision.NodeID], revision)
	}
	for _, edge := range snapshot.Edges {
		if nodes[edge.FromNode].ID == "" || nodes[edge.ToNode].ID == "" {
			return errors.New("edge references missing node")
		}
		edges[edge.ID] = edge
	}
	g.nodes, g.edges, g.revisions = nodes, edges, revisions
	return nil
}

func (g *Graph) persistLocked() error {
	if g.persistence == nil {
		return nil
	}
	return g.persistence(g.snapshotLocked())
}

func (g *Graph) snapshotLocked() Snapshot {
	snapshot := Snapshot{Nodes: g.nodesLocked(), Edges: g.edgesLocked()}
	for _, revisions := range g.revisions {
		snapshot.Revisions = append(snapshot.Revisions, cloneRevisions(revisions)...)
	}
	sort.Slice(snapshot.Revisions, func(i, j int) bool {
		if snapshot.Revisions[i].NodeID != snapshot.Revisions[j].NodeID {
			return snapshot.Revisions[i].NodeID < snapshot.Revisions[j].NodeID
		}
		return snapshot.Revisions[i].Revision < snapshot.Revisions[j].Revision
	})
	return snapshot
}

func (g *Graph) nodesLocked() []Node {
	nodes := make([]Node, 0, len(g.nodes))
	for _, node := range g.nodes {
		nodes = append(nodes, cloneNode(node))
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return nodes
}

func (g *Graph) edgesLocked() []Edge {
	edges := make([]Edge, 0, len(g.edges))
	for _, edge := range g.edges {
		edges = append(edges, edge)
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	return edges
}

func validVerification(evidence []Verification) bool {
	if len(evidence) == 0 {
		return false
	}
	for _, verification := range evidence {
		if verification.ID == "" || verification.Verifier == "" || !verification.Passed || verification.ExitCode != 0 || len(verification.ArtifactIDs) == 0 {
			return false
		}
	}
	return true
}

func allowedTransition(current, next string) bool {
	transitions := map[string][]string{
		StatusProposed:      {StatusInvestigating, StatusRefuted, StatusAbandoned},
		StatusInvestigating: {StatusSupported, StatusRefuted, StatusInconclusive, StatusAbandoned},
		StatusSupported:     {StatusVerified, StatusRefuted, StatusInvestigating},
		StatusRefuted:       {StatusInvestigating, StatusAbandoned},
		StatusInconclusive:  {StatusInvestigating, StatusAbandoned},
	}
	for _, allowed := range transitions[current] {
		if allowed == next {
			return true
		}
	}
	return false
}

func validNodeType(nodeType NodeType) bool {
	return nodeType == NodeObjective || nodeType == NodeDirection || nodeType == NodeHypothesis || nodeType == NodeClaim || nodeType == NodePaper || nodeType == NodeEvidence || nodeType == NodeExperiment || nodeType == NodeBenchmark || nodeType == NodeCounterexample || nodeType == NodeProof || nodeType == NodeRefutation || nodeType == NodeArtifact || nodeType == NodeConclusion
}

func validEdgeType(edgeType EdgeType) bool {
	return edgeType == EdgeDependsOn || edgeType == EdgeSupports || edgeType == EdgeContradicts || edgeType == EdgeDerivedFrom || edgeType == EdgeInvestigates || edgeType == EdgeVerifies || edgeType == EdgeRefutes || edgeType == EdgeImplements || edgeType == EdgeCites
}

func cloneNode(node Node) Node {
	node.RelatedTasks = append([]string(nil), node.RelatedTasks...)
	node.Artifacts = append([]string(nil), node.Artifacts...)
	node.EvidenceRefs = append([]string(nil), node.EvidenceRefs...)
	return node
}

func cloneRevisions(revisions []Revision) []Revision {
	cloned := append([]Revision(nil), revisions...)
	for i := range cloned {
		cloned[i].Node = cloneNode(cloned[i].Node)
	}
	return cloned
}
