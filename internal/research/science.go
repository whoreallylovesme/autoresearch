package research

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/artifact"
	"github.com/whoreallylovesme/autoresearch/internal/graph"
	"github.com/whoreallylovesme/autoresearch/internal/knowledge"
	"github.com/whoreallylovesme/autoresearch/internal/literature"
	"github.com/whoreallylovesme/autoresearch/internal/storage"
)

type runtimeScience struct {
	graph     *graph.Graph
	knowledge *knowledge.Store
	fetcher   *literature.Fetcher
}

func newRuntimeScience(runID, objective string) *runtimeScience {
	researchGraph := graph.New()
	_ = researchGraph.AddNode(graph.Node{ID: "objective-" + runID, RunID: runID, Type: graph.NodeObjective, Author: "operator", Status: "active", Description: objective, CreatedAt: time.Now().UTC()})
	return &runtimeScience{graph: researchGraph}
}

func (r *Runtime) attachPersistentScience(store *storage.SQLite, artifacts *artifact.Store) error {
	r.science.knowledge = knowledge.New(r.status.ID, store)
	var source literature.Source
	if r.configuration.Chief.Model.Provider == "mock" {
		source = literature.NewStaticSource("demo-static", demoPapers())
	} else {
		source = literature.NewArxivSource(nil, "")
	}
	r.science.fetcher = literature.New(r.science.knowledge, artifacts, nil, source)
	r.science.graph.SetPersistence(func(snapshot graph.Snapshot) error {
		return store.SaveGraph(context.Background(), r.status.ID, snapshot)
	})
	snapshot, err := store.LoadGraph(context.Background(), r.status.ID)
	if err != nil {
		return err
	}
	if len(snapshot.Nodes) > 0 {
		return r.science.graph.Restore(snapshot)
	}
	return nil
}

func (r *Runtime) saveScienceGraph() error {
	if r.persistence == nil || r.science == nil {
		return nil
	}
	return r.persistence.store.SaveGraph(context.Background(), r.status.ID, r.science.graph.Snapshot())
}

func (r *Runtime) Graph() graph.Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.science == nil {
		return graph.Snapshot{}
	}
	return r.science.graph.Snapshot()
}

func (r *Runtime) Papers(ctx context.Context) ([]knowledge.Paper, error) {
	if r.science == nil || r.science.knowledge == nil {
		return nil, nil
	}
	return r.science.knowledge.ListPapers(ctx)
}

func (r *Runtime) executeScienceTool(ctx context.Context, agentID, name string, arguments json.RawMessage) (bool, json.RawMessage, error) {
	if r.science == nil {
		return false, nil, nil
	}
	switch name {
	case "search_papers":
		var input struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := decode(arguments, &input); err != nil {
			return true, nil, err
		}
		if input.Limit == 0 {
			input.Limit = 5
		}
		if r.science.fetcher == nil {
			return true, nil, errors.New("paper fetcher requires persistent knowledge storage")
		}
		papers, err := r.science.fetcher.Search(ctx, input.Query, input.Limit)
		if err != nil {
			return true, nil, err
		}
		for _, paper := range papers {
			r.addPaperNode(agentID, paper)
			paper.FullText = ""
		}
		result, err := json.Marshal(map[string]any{"papers": papers})
		return true, result, err
	case "read_paper":
		var input struct {
			PaperID string `json:"paper_id"`
		}
		if err := decode(arguments, &input); err != nil {
			return true, nil, err
		}
		if r.science.knowledge == nil {
			return true, nil, errors.New("knowledge base requires persistent storage")
		}
		paper, err := r.science.knowledge.Paper(ctx, input.PaperID)
		if err != nil {
			return true, nil, err
		}
		claims, err := r.science.knowledge.Claims(ctx, input.PaperID)
		if err != nil {
			return true, nil, err
		}
		truncated := len(paper.FullText) > 65536
		if truncated {
			paper.FullText = paper.FullText[:65536]
		}
		result, err := json.Marshal(map[string]any{"paper": paper, "claims": claims, "full_text_truncated": truncated})
		return true, result, err
	case "register_hypothesis":
		var input struct {
			ID          string   `json:"id"`
			Description string   `json:"description"`
			TaskIDs     []string `json:"task_ids"`
		}
		if err := decode(arguments, &input); err != nil {
			return true, nil, err
		}
		if input.ID == "" || strings.TrimSpace(input.Description) == "" {
			return true, nil, errors.New("hypothesis id and description are required")
		}
		node := graph.Node{ID: input.ID, RunID: r.status.ID, Type: graph.NodeHypothesis, Author: agentID, Status: graph.StatusProposed, Description: input.Description, RelatedTasks: input.TaskIDs}
		if err := r.science.graph.AddNode(node); err != nil {
			return true, nil, err
		}
		r.mu.Lock()
		r.recordEventLocked("hypothesis.proposed", agentID, "", node)
		r.mu.Unlock()
		result, err := json.Marshal(map[string]string{"id": input.ID, "status": graph.StatusProposed})
		return true, result, err
	}
	return false, nil, nil
}

func (r *Runtime) searchKnowledge(ctx context.Context, query string) ([]knowledge.Paper, error) {
	if r.science == nil || r.science.knowledge == nil || query == "" || query == "all findings" {
		return nil, nil
	}
	return r.science.knowledge.Search(ctx, query, 10)
}

func (r *Runtime) recordScientificClaim(agentID, taskID string, input findingArguments) ([]string, error) {
	if input.PaperID == "" {
		return input.Evidence, nil
	}
	if r.science == nil || r.science.knowledge == nil {
		return nil, errors.New("paper citations require persistent knowledge storage")
	}
	paper, err := r.science.knowledge.Paper(context.Background(), input.PaperID)
	if err != nil {
		return nil, err
	}
	claimType := input.ClaimType
	if claimType == "" {
		claimType = knowledge.ClaimInterpretation
	}
	claim, err := r.science.knowledge.AddClaimRecord(context.Background(), knowledge.Claim{RunID: r.status.ID, PaperID: input.PaperID, Type: claimType, Text: input.Summary, Page: input.Page, Section: input.Section, Evidence: input.Evidence, Author: agentID})
	if err != nil {
		return nil, err
	}
	node := graph.Node{ID: claim.ID, RunID: r.status.ID, Type: graph.NodeClaim, Author: agentID, Status: claim.Type, Description: claim.Text, RelatedTasks: []string{taskID}, EvidenceRefs: claim.Evidence}
	if err := r.science.graph.AddNode(node); err != nil {
		return nil, err
	}
	r.addPaperNode(agentID, paper)
	if err := r.science.graph.AddEdge(graph.Edge{ID: "cites-" + claim.ID, RunID: r.status.ID, FromNode: claim.ID, ToNode: paper.ID, Type: graph.EdgeCites, Author: agentID}); err != nil {
		return nil, err
	}
	location := input.Section
	if location == "" {
		location = input.Page
	}
	citation := paper.SourceID
	if paper.ArxivID != "" {
		citation = "arXiv:" + paper.ArxivID
	}
	if location != "" {
		citation += ", " + location
	}
	return append(input.Evidence, citation), nil
}

func (r *Runtime) addPaperNode(agentID string, paper knowledge.Paper) {
	if _, exists := r.science.graph.Node(paper.ID); exists {
		return
	}
	_ = r.science.graph.AddNode(graph.Node{ID: paper.ID, RunID: r.status.ID, Type: graph.NodePaper, Author: agentID, Status: "available", Description: fmt.Sprintf("%s — %s:%s", paper.Title, paper.Source, paper.SourceID)})
}

func (r *Runtime) recordConclusion() {
	if r.science == nil {
		return
	}
	id := "conclusion-final"
	if _, exists := r.science.graph.Node(id); exists {
		return
	}
	_ = r.science.graph.AddNode(graph.Node{ID: id, RunID: r.status.ID, Type: graph.NodeConclusion, Author: "chief", Status: "completed", Description: r.report})
}

func demoPapers() []knowledge.Paper {
	return []knowledge.Paper{
		{SourceID: "2308.08155", ArxivID: "2308.08155", DOI: "10.48550/arXiv.2308.08155", Title: "AutoGen: Enabling Next-Gen LLM Applications via Multi-Agent Conversation", Abstract: "Bundled bibliographic metadata for deterministic testing. Consult the linked arXiv record for the authoritative abstract.", Authors: []string{"Qingyun Wu et al."}, PublishedAt: time.Date(2023, 8, 16, 0, 0, 0, 0, time.UTC), URL: "https://arxiv.org/abs/2308.08155", PDFURL: "https://arxiv.org/pdf/2308.08155"},
		{SourceID: "2303.17760", ArxivID: "2303.17760", Title: "CAMEL: Communicative Agents for Mind Exploration of Large Scale Language Model Society", Abstract: "Bundled bibliographic metadata for deterministic testing. Consult the linked arXiv record for the authoritative abstract.", Authors: []string{"Guohao Li et al."}, PublishedAt: time.Date(2023, 3, 31, 0, 0, 0, 0, time.UTC), URL: "https://arxiv.org/abs/2303.17760", PDFURL: "https://arxiv.org/pdf/2303.17760"},
	}
}
