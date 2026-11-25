# Scientific Knowledge Base

The knowledge base stores papers, normalized metadata, full text when legally and technically available, claims, methods, limitations, citations, research notes, experiment results, negative results, and code artifacts.

## Paper Fetcher

A centralized fetcher owns source clients, cache, deduplication, rate limits, retries, and artifact writes. Canonical DOI and arXiv identifiers are deduplication keys; source-specific IDs and normalized titles provide secondary matching. A fetch requested concurrently by many agents shares one result.

Initial sources are arXiv and OpenAlex-compatible HTTP APIs behind source interfaces. Semantic Scholar, Crossref, and PubMed adapters are configurable extensions. Every adapter must respect authentication, terms, rate limits, and attribution. No source is assumed available.

## Provenance

Each item retains source, source identifier, URL, fetch time, metadata revision, checksum, and extraction method. Findings distinguish exact paper claims with page/section locations from agent interpretations, hypotheses, and experimentally verified conclusions.

## Search

SQLite FTS5 indexes titles, abstracts, authors, extracted text, claims, and notes. Queries support source and metadata filters. Embeddings are deferred until measured research needs justify their model, storage, and reproducibility costs.

## Trust boundary

Paper text, PDFs, web pages, and metadata are untrusted input. They cannot change system prompts, grant tools, or trigger runtime operations. PDF processing runs with resource limits and without network or secret access.

## Retention

Artifacts and extracted text are immutable by checksum. Corrections create new revisions with provenance. Private research databases and public source code are separate storage and publication domains.
