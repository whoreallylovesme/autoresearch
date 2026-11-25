package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/graph"
)

func (s *SQLite) SaveGraph(ctx context.Context, runID string, snapshot graph.Snapshot) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		for _, table := range []string{"graph_edges", "graph_node_revisions", "graph_nodes"} {
			if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE run_id = ?", runID); err != nil {
				return err
			}
		}
		for _, node := range snapshot.Nodes {
			data, err := json.Marshal(node)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO graph_nodes(run_id, id, type, status, revision, data_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)", runID, node.ID, node.Type, node.Status, node.Revision, data, node.CreatedAt.UnixNano()); err != nil {
				return err
			}
		}
		for _, revision := range snapshot.Revisions {
			data, err := json.Marshal(revision)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO graph_node_revisions(run_id, node_id, revision, author, data_json, created_at) VALUES (?, ?, ?, ?, ?, ?)", runID, revision.NodeID, revision.Revision, revision.Author, data, revision.CreatedAt.UnixNano()); err != nil {
				return err
			}
		}
		for _, edge := range snapshot.Edges {
			data, err := json.Marshal(edge)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO graph_edges(run_id, id, from_node, to_node, type, data_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)", runID, edge.ID, edge.FromNode, edge.ToNode, edge.Type, data, edge.CreatedAt.UnixNano()); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *SQLite) LoadGraph(ctx context.Context, runID string) (graph.Snapshot, error) {
	var snapshot graph.Snapshot
	nodes, err := s.db.QueryContext(ctx, "SELECT data_json FROM graph_nodes WHERE run_id = ? ORDER BY id", runID)
	if err != nil {
		return snapshot, err
	}
	defer nodes.Close()
	for nodes.Next() {
		var data []byte
		if err := nodes.Scan(&data); err != nil {
			return snapshot, err
		}
		var node graph.Node
		if err := json.Unmarshal(data, &node); err != nil {
			return snapshot, err
		}
		snapshot.Nodes = append(snapshot.Nodes, node)
	}
	if err := nodes.Err(); err != nil {
		return snapshot, err
	}
	revisions, err := s.db.QueryContext(ctx, "SELECT data_json FROM graph_node_revisions WHERE run_id = ? ORDER BY node_id, revision", runID)
	if err != nil {
		return snapshot, err
	}
	defer revisions.Close()
	for revisions.Next() {
		var data []byte
		if err := revisions.Scan(&data); err != nil {
			return snapshot, err
		}
		var revision graph.Revision
		if err := json.Unmarshal(data, &revision); err != nil {
			return snapshot, err
		}
		revision.CreatedAt = revision.CreatedAt.UTC()
		snapshot.Revisions = append(snapshot.Revisions, revision)
	}
	if err := revisions.Err(); err != nil {
		return snapshot, err
	}
	edges, err := s.db.QueryContext(ctx, "SELECT data_json FROM graph_edges WHERE run_id = ? ORDER BY id", runID)
	if err != nil {
		return snapshot, err
	}
	defer edges.Close()
	for edges.Next() {
		var data []byte
		if err := edges.Scan(&data); err != nil {
			return snapshot, err
		}
		var edge graph.Edge
		if err := json.Unmarshal(data, &edge); err != nil {
			return snapshot, err
		}
		snapshot.Edges = append(snapshot.Edges, edge)
	}
	if err := edges.Err(); err != nil {
		return snapshot, err
	}
	for index := range snapshot.Revisions {
		snapshot.Revisions[index].CreatedAt = time.Unix(0, snapshot.Revisions[index].CreatedAt.UnixNano()).UTC()
	}
	return snapshot, nil
}
