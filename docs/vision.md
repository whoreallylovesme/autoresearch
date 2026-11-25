# Vision

AutoResearch is a single-node, open-source control plane for autonomous scientific work. It coordinates language-model agents, deterministic policy enforcement, durable scientific state, reproducible execution, and human oversight.

## Problem

Long-running research cannot be built on an ever-growing chat transcript. Models lose context, processes crash, tasks may execute twice, claims need evidence, and unrestricted tool access is unsafe. The scientific record must therefore be independent of any model session.

## Goals

- Support configurable Chief Scientist and worker fleets without hardcoded roles or headcounts.
- Run unattended for hours or days inside preauthorized technical boundaries.
- Preserve tasks, messages, events, evidence, and artifacts across process failures.
- Make every consequential model action pass through a validated runtime tool.
- Reserve and reconcile budgets across concurrent requests.
- Produce inspectable research graphs and reproducible reports with negative results.
- Run on a modest VPS, adding remote SSH or Slurm resources only by configuration.

## Non-goals

The project is not a chatbot platform, inference engine, training framework, Kubernetes replacement, autonomous paper fabrication system, or production-scale distributed computing platform. It does not replace researcher accountability or disclosure of AI-assisted work.

## Success measures

A release is successful when an interrupted run resumes without duplicating completed results, a critic can independently inspect evidence, resource policies survive malicious or confused model output, and the final report distinguishes verification from interpretation.

## Status

This document describes the target architecture. `docs/progress.md` identifies implemented behavior.
