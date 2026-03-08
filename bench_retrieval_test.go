package main

import (
	"fmt"
	"testing"
	"time"
)

// benchMemory defines a memory for the retrieval quality benchmark.
// Each memory has a known ID (based on insertion order: m1, m2, ...) so that
// test cases can reference specific expected matches.
type benchMemory struct {
	text    string
	label   string
	memType string // "fact", "decision", "note", etc.
}

// retrievalTestCase defines a query and its expected relevant memories.
type retrievalTestCase struct {
	query    string
	tier     string   // "easy", "medium", "hard"
	relevant []string // IDs of memories that are correct matches (e.g. "m1", "m15")
}

// benchmarkMemories is the curated dataset of realistic memories covering:
// user preferences, project facts, architecture decisions, status updates,
// coding patterns/principles, tool configurations, relationships, and notes.
//
// IDs are m1..m65 based on insertion order via Add(). Each comment shows the
// expected ID for cross-reference by test cases.
var benchmarkMemories = []benchMemory{
	// ── User preferences & identity (m1–m8) ──────────────────────────────

	{ // m1
		text:    "#self I prefer dark mode in all editors and terminals. My go-to color scheme is Catppuccin Mocha.",
		label:   "dark mode preference",
		memType: "fact",
	},
	{ // m2
		text:    "#self I use neovim as my primary editor with lazy.nvim for plugin management. Cursor is my secondary IDE for AI-assisted coding.",
		label:   "editor setup",
		memType: "fact",
	},
	{ // m3
		text:    "#self My preferred programming languages are Go and TypeScript. I avoid Java when possible.",
		label:   "language preferences",
		memType: "fact",
	},
	{ // m4
		text:    "#self I prefer functional error handling over exceptions. In Go I always wrap errors with fmt.Errorf and %w verb.",
		label:   "error handling style",
		memType: "fact",
	},
	{ // m5
		text:    "#self I like code comments that explain 'why' not 'what'. Comments that merely restate the code are noise.",
		label:   "commenting philosophy",
		memType: "fact",
	},
	{ // m6
		text:    "#self I use tabs for indentation in Go files and 2-space indentation in TypeScript, JavaScript, and YAML.",
		label:   "indentation style",
		memType: "fact",
	},
	{ // m7
		text:    "#self I am most productive in the morning between 6am and noon. I prefer asynchronous communication over meetings.",
		label:   "work schedule",
		memType: "fact",
	},
	{ // m8
		text:    "#self My GitHub username is devstream42. I authenticate with SSH keys for git, never HTTPS personal access tokens.",
		label:   "github identity",
		memType: "fact",
	},

	// ── Project facts (m9–m20) ───────────────────────────────────────────

	{ // m9
		text:    "#fact The llmem project is a persistent AI memory system built in Go. It uses TF-IDF cosine similarity for text search and retrieval.",
		label:   "llmem overview",
		memType: "fact",
	},
	{ // m10
		text:    "#fact llmem stores data in SQLite with WAL mode enabled for concurrent read access. The database file lives at ~/.llmem/memories.db by default.",
		label:   "sqlite storage",
		memType: "fact",
	},
	{ // m11
		text:    "#fact The MCP server listens on stdin/stdout for JSON-RPC messages. It exposes tools like memory_add, memory_search, memory_get, and memory_relevant.",
		label:   "MCP protocol details",
		memType: "fact",
	},
	{ // m12
		text:    "#fact The frontend dashboard is a React app styled with Tailwind CSS, served on port 3000. It visualizes the memory similarity graph with D3.js.",
		label:   "frontend stack",
		memType: "fact",
	},
	{ // m13
		text:    "#fact Our API rate limiting uses a token bucket algorithm allowing 100 requests per minute per client IP address.",
		label:   "rate limiting config",
		memType: "fact",
	},
	{ // m14
		text:    "#fact The production database runs PostgreSQL 16 on AWS RDS with read replicas in us-east-1 and eu-west-1 regions.",
		label:   "production database",
		memType: "fact",
	},
	{ // m15
		text:    "#fact Authentication uses JWT tokens signed with RS256. Access tokens expire after 15 minutes, refresh tokens after 7 days.",
		label:   "JWT auth config",
		memType: "fact",
	},
	{ // m16
		text:    "#fact The search indexing pipeline processes documents through tokenization, stop word removal, stemming, and TF-IDF vectorization before storing them.",
		label:   "search pipeline",
		memType: "fact",
	},
	{ // m17
		text:    "#fact Docker images are built with multi-stage builds. The final Go binary runs on gcr.io/distroless/static-debian12 for minimal attack surface.",
		label:   "docker build strategy",
		memType: "fact",
	},
	{ // m18
		text:    "#fact CI/CD runs on GitHub Actions. Unit tests and linting run on every pull request. Deployment to production happens automatically on merge to main.",
		label:   "CI/CD pipeline",
		memType: "fact",
	},
	{ // m19
		text:    "#fact The project uses Go modules with minimum version Go 1.22. Dependencies are vendored with go mod vendor for reproducible builds.",
		label:   "go module config",
		memType: "fact",
	},
	{ // m20
		text:    "#fact Application logging uses structured JSON format via zerolog. Logs are shipped to Grafana Loki through Promtail for aggregation and alerting.",
		label:   "logging stack",
		memType: "fact",
	},

	// ── Architecture decisions (m21–m30) ─────────────────────────────────

	{ // m21
		text:    "#decision We chose SQLite over PostgreSQL for llmem because it is embedded, requires zero configuration, and the data model is single-user per installation.",
		label:   "sqlite over postgres",
		memType: "decision",
	},
	{ // m22
		text:    "#decision We use TF-IDF cosine similarity instead of neural embedding models to avoid external API dependencies and keep search latency under 5ms.",
		label:   "tfidf over embeddings",
		memType: "decision",
	},
	{ // m23
		text:    "#decision The similarity graph stores bidirectional edges in memory with a cosine threshold of 0.35. This balances recall against noise from weak connections.",
		label:   "similarity threshold",
		memType: "decision",
	},
	{ // m24
		text:    "#decision We chose the MCP protocol over a REST API because it integrates natively with AI assistants like Claude Desktop and Cursor.",
		label:   "MCP over REST",
		memType: "decision",
	},
	{ // m25
		text:    "#decision Error handling follows Go idioms: return errors up the call stack, wrap with context using fmt.Errorf, define sentinel errors for classified failures.",
		label:   "go error conventions",
		memType: "decision",
	},
	{ // m26
		text:    "#decision The caching layer uses Redis with a 5-minute TTL for API responses. Cache entries are invalidated eagerly on any write operation.",
		label:   "redis caching strategy",
		memType: "decision",
	},
	{ // m27
		text:    "#decision We migrated from a monolithic architecture to microservices in Q3 2025. The user service, order service, and payment service are now separate deployments.",
		label:   "microservices migration",
		memType: "decision",
	},
	{ // m28
		text:    "#decision Pagination uses cursor-based keyset pagination instead of OFFSET/LIMIT to maintain consistent performance on large result sets.",
		label:   "cursor pagination",
		memType: "decision",
	},
	{ // m29
		text:    "#decision Internal service-to-service communication uses Protocol Buffers over gRPC. External-facing APIs use JSON over HTTP.",
		label:   "protobuf for internal",
		memType: "decision",
	},
	{ // m30
		text:    "#decision Memory auto-consolidation merges near-duplicate memories using a 0.95 similarity threshold to prevent unbounded growth of the memory store.",
		label:   "consolidation threshold",
		memType: "decision",
	},

	// ── Status updates (m31–m38) ─────────────────────────────────────────

	{ // m31
		text:    "#status Currently working on improving retrieval quality for llmem search. Adding benchmark tests to measure precision, recall, and MRR.",
		label:   "retrieval quality work",
		memType: "status",
	},
	{ // m32
		text:    "#status The authentication refactor is 80% complete. OAuth2 provider integration is done; SAML support for enterprise SSO is still pending.",
		label:   "auth refactor progress",
		memType: "status",
	},
	{ // m33
		text:    "#status Deployed version 2.3.0 to production on 2025-12-15. Release includes auto-consolidation and memory scope filtering features.",
		label:   "v2.3.0 deploy",
		memType: "status",
	},
	{ // m34
		text:    "#status Fixed the performance regression on /api/search endpoint by adding an inverted token index. Latency dropped from 200ms to 8ms at p99.",
		label:   "search perf fix",
		memType: "status",
	},
	{ // m35
		text:    "#status Sprint 14 goals: finish retrieval benchmarks, add optional embedding support, and improve similarity edge rebuilding performance.",
		label:   "sprint 14 plan",
		memType: "status",
	},
	{ // m36
		text:    "#status The iOS mobile app release is blocked by Apple App Store review. Expected approval by end of this week.",
		label:   "mobile app blocked",
		memType: "status",
	},
	{ // m37
		text:    "#status Database migration from MySQL 5.7 to PostgreSQL 16 is scheduled for the Saturday 2am UTC maintenance window.",
		label:   "db migration scheduled",
		memType: "status",
	},
	{ // m38
		text:    "#status Code review backlog has 12 open pull requests. Priority items are the auth refactor PR #234 and the search improvements PR #241.",
		label:   "PR review backlog",
		memType: "status",
	},

	// ── Coding principles & patterns (m39–m48) ──────────────────────────

	{ // m39
		text:    "#principle Always write table-driven tests in Go. Each test case should have a descriptive name and cover both success and error paths.",
		label:   "table-driven tests",
		memType: "fact",
	},
	{ // m40
		text:    "#principle Keep functions under 50 lines of code. Extract well-named helper functions when a function grows beyond that limit.",
		label:   "function length limit",
		memType: "fact",
	},
	{ // m41
		text:    "#principle Database transactions must be as short as possible. Never hold a transaction open while making network calls to external services.",
		label:   "short transactions",
		memType: "fact",
	},
	{ // m42
		text:    "#principle Use dependency injection for all external service integrations. Accept interfaces as parameters, return concrete types.",
		label:   "dependency injection",
		memType: "fact",
	},
	{ // m43
		text:    "#principle Every public API endpoint must validate request input, enforce rate limits, and return structured JSON error responses with error codes.",
		label:   "API endpoint standards",
		memType: "fact",
	},
	{ // m44
		text:    "#principle Git commits must be atomic with one logical change per commit. Use conventional commit format: feat:, fix:, refactor:, docs:, test:, chore:.",
		label:   "commit conventions",
		memType: "fact",
	},
	{ // m45
		text:    "#principle Never store secrets, API keys, or credentials in source code or config files. Use environment variables or HashiCorp Vault for secrets management.",
		label:   "secrets management",
		memType: "fact",
	},
	{ // m46
		text:    "#principle All HTTP server handlers should configure explicit timeouts: read timeout 10s, write timeout 30s, idle timeout 120s.",
		label:   "HTTP timeout policy",
		memType: "fact",
	},
	{ // m47
		text:    "#principle Implement graceful shutdown: listen for SIGTERM, stop accepting new connections, drain in-flight requests with a 30-second deadline before exiting.",
		label:   "graceful shutdown",
		memType: "fact",
	},
	{ // m48
		text:    "#principle Always propagate cancellation via context.Context. Pass ctx as the first parameter to any function performing I/O or calling external services.",
		label:   "context propagation",
		memType: "fact",
	},

	// ── Tool & environment configuration (m49–m55) ──────────────────────

	{ // m49
		text:    "#fact My development machine runs Ubuntu 24.04 LTS with 32GB RAM and an NVMe SSD. I use Docker Engine (not Desktop) for containers.",
		label:   "dev machine specs",
		memType: "fact",
	},
	{ // m50
		text:    "#fact The project Makefile has targets: build, test, lint, run, docker-build, docker-push, migrate-up, migrate-down, and bench.",
		label:   "makefile targets",
		memType: "fact",
	},
	{ // m51
		text:    "#fact We run golangci-lint with these linters enabled: errcheck, gosimple, govet, ineffassign, staticcheck, unused, and revive.",
		label:   "linter configuration",
		memType: "fact",
	},
	{ // m52
		text:    "#fact The staging environment is accessible at staging.example.com. It mirrors production topology but uses a separate database with anonymized user data.",
		label:   "staging environment",
		memType: "fact",
	},
	{ // m53
		text:    "#fact Monitoring uses Prometheus for metrics collection exposed on the /metrics endpoint. Grafana dashboards track request latency, error rates, and throughput.",
		label:   "monitoring setup",
		memType: "fact",
	},
	{ // m54
		text:    "#fact The load balancer is nginx with upstream health checks every 10 seconds. TLS termination happens at the load balancer, internal traffic is plain HTTP.",
		label:   "nginx load balancer",
		memType: "fact",
	},
	{ // m55
		text:    "#fact Infrastructure is provisioned with Terraform. Remote state is stored in an S3 bucket with DynamoDB state locking to prevent concurrent modifications.",
		label:   "terraform setup",
		memType: "fact",
	},

	// ── People & relationships (m56–m60) ─────────────────────────────────

	{ // m56
		text:    "#relationship Sarah is the tech lead. She reviews all architecture decisions and expects a detailed RFC document before any major implementation begins.",
		label:   "Sarah - tech lead",
		memType: "fact",
	},
	{ // m57
		text:    "#relationship Alex and Jordan form the DevOps team. They manage the Kubernetes cluster and CI/CD pipelines. Reach them in the #infra-support Slack channel.",
		label:   "DevOps team",
		memType: "fact",
	},
	{ // m58
		text:    "#relationship Mike is the product manager. He tracks work in Linear and runs a weekly sync every Tuesday at 10am Eastern time.",
		label:   "Mike - product manager",
		memType: "fact",
	},
	{ // m59
		text:    "#relationship The data science team consists of Lisa, Tom, and Carlos. They own the recommendation engine, primarily code in Python, and coordinate in #ml-team Slack.",
		label:   "data science team",
		memType: "fact",
	},
	{ // m60
		text:    "#relationship Priya is the QA lead responsible for the end-to-end test suite in Playwright. Bug reports are filed in the Jira project with key QA.",
		label:   "Priya - QA lead",
		memType: "fact",
	},

	// ── Goals, thoughts & notes (m61–m65) ────────────────────────────────

	{ // m61
		text:    "#thought The current TF-IDF approach handles exact keyword matches well but struggles with synonyms and paraphrases. Adding lightweight embedding vectors as a complementary signal could significantly improve semantic retrieval.",
		label:   "embedding improvement idea",
		memType: "note",
	},
	{ // m62
		text:    "#goal By Q2 2026 llmem should support 10,000 or more memories with sub-10ms search latency and above 90% retrieval accuracy on common query patterns.",
		label:   "Q2 2026 target",
		memType: "note",
	},
	{ // m63
		text:    "#fact The WebSocket server for real-time notifications uses gorilla/websocket. Each connected client gets a dedicated read and write goroutine pair.",
		label:   "websocket architecture",
		memType: "fact",
	},
	{ // m64
		text:    "#note When debugging memory leaks in Go use pprof: go tool pprof http://localhost:6060/debug/pprof/heap. Also check for goroutine leaks with /debug/pprof/goroutine.",
		label:   "pprof debugging tip",
		memType: "note",
	},
	{ // m65
		text:    "#note The daily backup script runs at 3am UTC via cron. It creates a SQLite dump at /backups/llmem-YYYY-MM-DD.db and syncs the file to an S3 bucket.",
		label:   "backup schedule",
		memType: "note",
	},
}

// retrievalTestCases defines queries across three difficulty tiers with
// ground-truth relevant memory IDs. Each query is designed to test a
// specific retrieval challenge.
//
// Tier counts: 15 easy + 15 medium + 15 hard = 45 total.
var retrievalTestCases = []retrievalTestCase{
	// ═══════════════════════════════════════════════════════════════════
	// EASY TIER (~15 cases)
	//
	// Direct lexical overlap: the query reuses distinctive tokens from
	// the target memory. TF-IDF cosine similarity and lexical overlap
	// should both rank these highly.
	// ═══════════════════════════════════════════════════════════════════

	{ // E1: m1 — "dark mode", "Catppuccin Mocha", "color scheme"
		query:    "dark mode Catppuccin Mocha color scheme",
		tier:     "easy",
		relevant: []string{"m1"},
	},
	{ // E2: m2 — "neovim", "lazy.nvim", "plugin management"
		query:    "neovim lazy.nvim plugin management",
		tier:     "easy",
		relevant: []string{"m2"},
	},
	{ // E3: m10 — "SQLite", "WAL mode", "memories.db"
		query:    "SQLite WAL mode database file path",
		tier:     "easy",
		relevant: []string{"m10"},
	},
	{ // E4: m15 — "JWT", "RS256", "access tokens", "refresh tokens"
		query:    "JWT tokens RS256 access token refresh token expiration",
		tier:     "easy",
		relevant: []string{"m15"},
	},
	{ // E5: m17 — "Docker", "multi-stage builds", "distroless"
		query:    "Docker multi-stage build distroless image",
		tier:     "easy",
		relevant: []string{"m17"},
	},
	{ // E6: m18 — "GitHub Actions", "CI/CD", "pull request", "deployment"
		query:    "GitHub Actions CI/CD pull request deployment",
		tier:     "easy",
		relevant: []string{"m18"},
	},
	{ // E7: m26 — "Redis", "TTL", "cache", "invalidated"
		query:    "Redis caching TTL invalidation strategy",
		tier:     "easy",
		relevant: []string{"m26"},
	},
	{ // E8: m29 — "Protocol Buffers", "gRPC", "JSON over HTTP"
		query:    "Protocol Buffers gRPC internal service communication",
		tier:     "easy",
		relevant: []string{"m29"},
	},
	{ // E9: m51 — "golangci-lint", "errcheck", "staticcheck", "revive"
		query:    "golangci-lint errcheck staticcheck linters enabled",
		tier:     "easy",
		relevant: []string{"m51"},
	},
	{ // E10: m53 — "Prometheus", "metrics", "Grafana", "dashboards"
		query:    "Prometheus metrics Grafana dashboards latency",
		tier:     "easy",
		relevant: []string{"m53"},
	},
	{ // E11: m55 — "Terraform", "S3", "DynamoDB", "state locking"
		query:    "Terraform S3 DynamoDB state locking infrastructure",
		tier:     "easy",
		relevant: []string{"m55"},
	},
	{ // E12: m64 — "pprof", "heap", "goroutine"
		query:    "pprof heap goroutine leak debugging Go",
		tier:     "easy",
		relevant: []string{"m64"},
	},
	{ // E13: m11 — "MCP server", "JSON-RPC", "stdin/stdout", "memory_add"
		query:    "MCP server JSON-RPC stdin stdout memory_add",
		tier:     "easy",
		relevant: []string{"m11"},
	},
	{ // E14: m39 — "table-driven tests", "Go", "success and error paths"
		query:    "table-driven tests in Go with success and error paths",
		tier:     "easy",
		relevant: []string{"m39"},
	},
	{ // E15: m65 — "backup", "cron", "SQLite dump", "S3 bucket"
		query:    "daily backup cron SQLite dump S3 bucket",
		tier:     "easy",
		relevant: []string{"m65"},
	},

	// ═══════════════════════════════════════════════════════════════════
	// MEDIUM TIER (~15 cases)
	//
	// Partial token overlap with competing distractors. The correct
	// memory shares some words with the query, but other memories also
	// share tokens, making ranking quality the deciding factor.
	// ═══════════════════════════════════════════════════════════════════

	{ // M1: m16 (search indexing pipeline) — competes with m9, m22, m34 which also mention "search"
		query:    "how are documents preprocessed before indexing",
		tier:     "medium",
		relevant: []string{"m16"},
	},
	{ // M2: m10 (SQLite storage) and m21 (SQLite decision) both answer this;
		// competes with m14 (PostgreSQL), m37 (database migration)
		query:    "which database does llmem use for storage",
		tier:     "medium",
		relevant: []string{"m10", "m21"},
	},
	{ // M3: m4 (error handling style) and m25 (Go error conventions) both match;
		// competes with m3 (language prefs mention Go), m19 (Go modules)
		query:    "error handling approach in Go projects",
		tier:     "medium",
		relevant: []string{"m4", "m25"},
	},
	{ // M4: m15 (JWT auth config); competes with m32 (auth refactor status)
		query:    "how does authentication work and what tokens are used",
		tier:     "medium",
		relevant: []string{"m15"},
	},
	{ // M5: m27 (microservices migration); competes with m29 (gRPC inter-service)
		query:    "microservices architecture migration decision",
		tier:     "medium",
		relevant: []string{"m27"},
	},
	{ // M6: m45 (secrets management); competes with m8 (SSH keys), m15 (JWT auth)
		query:    "best practices for managing sensitive credentials",
		tier:     "medium",
		relevant: []string{"m45"},
	},
	{ // M7: m54 (nginx, TLS termination); partial overlap on "TLS" only
		query:    "how does TLS termination and traffic routing work",
		tier:     "medium",
		relevant: []string{"m54"},
	},
	{ // M8: m19 (Go 1.22, go mod vendor); competes with m3 (Go preference), m9 (built in Go)
		query:    "what version of Go does the project require",
		tier:     "medium",
		relevant: []string{"m19"},
	},
	{ // M9: m57 (Alex & Jordan, DevOps); competes with m55 (Terraform), m18 (CI/CD)
		query:    "who manages infrastructure and Kubernetes",
		tier:     "medium",
		relevant: []string{"m57"},
	},
	{ // M10: m38 (PR review backlog); competes with m18 (CI/CD mentions pull request)
		query:    "open pull requests that need review",
		tier:     "medium",
		relevant: []string{"m38"},
	},
	{ // M11: m35 (sprint 14 goals); competes with m31 (currently working on), m62 (Q2 goal)
		query:    "what are the current sprint objectives",
		tier:     "medium",
		relevant: []string{"m35"},
	},
	{ // M12: m12 (React, Tailwind, D3.js dashboard); "web UI" and "framework" are indirect
		query:    "how is the web UI built and what framework does it use",
		tier:     "medium",
		relevant: []string{"m12"},
	},
	{ // M13: m13 (token bucket, 100 req/min); "throttling" vs "rate limiting"
		query:    "request throttling and rate limit configuration",
		tier:     "medium",
		relevant: []string{"m13"},
	},
	{ // M14: m28 (cursor-based keyset pagination); competes with database memories
		query:    "efficient pagination strategy for large datasets",
		tier:     "medium",
		relevant: []string{"m28"},
	},
	{ // M15: m20 (zerolog, Grafana Loki, Promtail); competes with m53 (Prometheus/Grafana)
		query:    "what logging framework and log aggregation do we use",
		tier:     "medium",
		relevant: []string{"m20"},
	},

	// ═══════════════════════════════════════════════════════════════════
	// HARD TIER (~15 cases)
	//
	// Semantic gap: query and target use different words for the same
	// concept. Tests synonym handling, paraphrase understanding, and
	// conceptual relationships. TF-IDF is expected to struggle here;
	// embedding-based retrieval should show the biggest improvements.
	// ═══════════════════════════════════════════════════════════════════

	{ // H1: m30 — "auto-consolidation merges near-duplicate memories…unbounded growth"
		// Query: "repetitive" ≠ "near-duplicate", "buildup" ≠ "unbounded growth"
		query:    "prevent repetitive information buildup in the store",
		tier:     "hard",
		relevant: []string{"m30"},
	},
	{ // H2: m40 — "Keep functions under 50 lines…extract helper functions"
		// Query: "subroutine" ≠ "function", "size" ≠ "50 lines"
		query:    "subroutine size guidelines",
		tier:     "hard",
		relevant: []string{"m40"},
	},
	{ // H3: m42 — "dependency injection…accept interfaces as parameters"
		// Query: "inversion of control" — same OOP concept, zero token overlap
		query:    "inversion of control pattern for external integrations",
		tier:     "hard",
		relevant: []string{"m42"},
	},
	{ // H4: m7 — "most productive in the morning between 6am and noon"
		// Query: "optimal" ≠ "productive", "focused coding" ≠ "productive"
		query:    "optimal daily schedule for focused coding",
		tier:     "hard",
		relevant: []string{"m7"},
	},
	{ // H5: m58 — "Mike…product manager…weekly sync every Tuesday at 10am"
		// Query: "check-in cadence" ≠ "weekly sync", "product owner" ≈ "product manager"
		query:    "regular check-in cadence with the product owner",
		tier:     "hard",
		relevant: []string{"m58"},
	},
	{ // H6: m5 — "code comments that explain 'why' not 'what'"
		// Query: "documentation" ≠ "comments", "annotations" ≠ "comments", "source files" ≠ "code"
		query:    "documentation and annotation philosophy for source files",
		tier:     "hard",
		relevant: []string{"m5"},
	},
	{ // H7: m44 — "atomic…one logical change per commit…conventional commit format"
		// Query: "version history" ≠ "Git commits", "clean" ≠ "atomic"
		query:    "keeping version history clean and meaningful",
		tier:     "hard",
		relevant: []string{"m44"},
	},
	{ // H8: m47 — "graceful shutdown…SIGTERM…drain in-flight requests"
		// Query: "orderly" ≠ "graceful", "termination" ≠ "shutdown"
		query:    "orderly process termination under load",
		tier:     "hard",
		relevant: []string{"m47"},
	},
	{ // H9: m27 — "migrated from monolithic architecture to microservices"
		// Query: "decomposing" ≠ "migrated", "unified codebase" ≠ "monolithic",
		// "independently deployable units" ≠ "microservices"
		query:    "decomposing a unified codebase into independently deployable units",
		tier:     "hard",
		relevant: []string{"m27"},
	},
	{ // H10: m21 — "chose SQLite over PostgreSQL because it is embedded"
		// Query: "in-process" ≠ "embedded", "data store" ≠ "SQLite"
		query:    "rationale for running the data store in-process",
		tier:     "hard",
		relevant: []string{"m21"},
	},
	{ // H11: m45 — "Never store secrets, API keys, or credentials in source code"
		// Query: "safeguarding" ≠ "never store", "runtime config" ≠ "secrets",
		// "leaking" ≠ "store…in source code"
		query:    "safeguarding runtime config values from leaking into repositories",
		tier:     "hard",
		relevant: []string{"m45"},
	},
	{ // H12: m53 — "Prometheus…metrics…Grafana dashboards…latency, error rates"
		// Query: "service health visibility" ≠ "monitoring", "uptime" ≠ "throughput"
		query:    "service health visibility and uptime tracking",
		tier:     "hard",
		relevant: []string{"m53"},
	},
	{ // H13: m64 — "pprof…heap…goroutine leaks"
		// Query: "profiling" ≠ "pprof", "resource consumption" ≠ "memory leaks"
		query:    "profiling resource consumption in the backend",
		tier:     "hard",
		relevant: []string{"m64"},
	},
	{ // H14: m34 — "Fixed the performance regression on /api/search…Latency dropped from 200ms to 8ms"
		// Query: "slow responses" ≠ "performance regression", "resolved" ≠ "fixed"
		query:    "what caused the slow endpoint responses and how was it resolved",
		tier:     "hard",
		relevant: []string{"m34"},
	},
	{ // H15: m61 — "TF-IDF…struggles with synonyms and paraphrases…embedding vectors"
		// Query: "related concepts" ≠ "synonyms", "phrasing differs" ≠ "paraphrases"
		query:    "finding related concepts even when phrasing differs",
		tier:     "hard",
		relevant: []string{"m61"},
	},
}

// populateBenchStore creates a MemoryStore and fills it with benchmarkMemories.
// Returns the store. Memory IDs will be m1..m65 in insertion order.
func populateBenchStore(t testing.TB) *MemoryStore {
	t.Helper()
	s, err := NewMemoryStore(MemoryStoreOptions{})
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	for i, bm := range benchmarkMemories {
		_, _, err := s.Add(bm.text, bm.label, bm.memType, nil)
		if err != nil {
			t.Fatalf("Add memory %d: %v", i+1, err)
		}
	}
	return s
}

// TestBenchmarkMemoriesLoad validates that all curated memories load correctly
// and receive the expected sequential IDs (m1..m65).
func TestBenchmarkMemoriesLoad(t *testing.T) {
	s := populateBenchStore(t)

	// Verify count.
	items := s.List("", "")
	if got, want := len(items), len(benchmarkMemories); got != want {
		t.Fatalf("store has %d memories, want %d", got, want)
	}

	// Verify each ID exists and text matches.
	for i, bm := range benchmarkMemories {
		id := fmt.Sprintf("m%d", i+1)
		chunk, _, err := s.Get(id)
		if err != nil {
			t.Errorf("Get(%s): %v", id, err)
			continue
		}
		if chunk.Text != bm.text {
			t.Errorf("m%d text mismatch:\n  got:  %s\n  want: %s", i+1, chunk.Text, bm.text)
		}
		if chunk.Type != bm.memType {
			t.Errorf("m%d type = %q, want %q", i+1, chunk.Type, bm.memType)
		}
	}

	// Quick distribution check.
	typeCounts := make(map[string]int)
	for _, bm := range benchmarkMemories {
		typeCounts[bm.memType]++
	}
	t.Logf("Memory count: %d", len(benchmarkMemories))
	for typ, n := range typeCounts {
		t.Logf("  type %-10s: %d", typ, n)
	}
}

// ═══════════════════════════════════════════════════════════════════════
// Retrieval quality metrics: MRR, Recall@k, Precision@k
// ═══════════════════════════════════════════════════════════════════════

// queryMetrics holds per-query retrieval quality scores.
type queryMetrics struct {
	tier         string
	query        string
	recipRank    float64 // 1/rank of first relevant result (0 if not found)
	recallAt3    float64 // 1.0 if any relevant in top-3, else 0.0
	recallAt5    float64
	precisionAt3 float64 // fraction of top-3 that are relevant
}

// tierReport holds aggregated metrics for a single tier (or "overall").
type tierReport struct {
	tier         string
	mrr          float64
	recallAt3    float64
	recallAt5    float64
	precisionAt3 float64
	count        int
}

// computeQueryMetrics computes MRR, Recall@k, and Precision@k for a single
// query given the ordered list of result IDs returned by the retrieval method.
func computeQueryMetrics(tc retrievalTestCase, resultIDs []string) queryMetrics {
	relevant := make(map[string]struct{}, len(tc.relevant))
	for _, id := range tc.relevant {
		relevant[id] = struct{}{}
	}

	// Reciprocal rank: 1/rank of first relevant result.
	rr := 0.0
	for i, id := range resultIDs {
		if _, ok := relevant[id]; ok {
			rr = 1.0 / float64(i+1)
			break
		}
	}

	return queryMetrics{
		tier:         tc.tier,
		query:        tc.query,
		recipRank:    rr,
		recallAt3:    recallAtK(resultIDs, relevant, 3),
		recallAt5:    recallAtK(resultIDs, relevant, 5),
		precisionAt3: precisionAtK(resultIDs, relevant, 3),
	}
}

// recallAtK returns 1.0 if at least one relevant ID appears in the top-k results.
func recallAtK(resultIDs []string, relevant map[string]struct{}, k int) float64 {
	n := k
	if n > len(resultIDs) {
		n = len(resultIDs)
	}
	for i := 0; i < n; i++ {
		if _, ok := relevant[resultIDs[i]]; ok {
			return 1.0
		}
	}
	return 0.0
}

// precisionAtK returns the fraction of top-k results that are relevant.
// Denominator is always k (not min(k, len)), penalizing sparse result sets.
func precisionAtK(resultIDs []string, relevant map[string]struct{}, k int) float64 {
	if k == 0 {
		return 0.0
	}
	n := k
	if n > len(resultIDs) {
		n = len(resultIDs)
	}
	var hits float64
	for i := 0; i < n; i++ {
		if _, ok := relevant[resultIDs[i]]; ok {
			hits++
		}
	}
	return hits / float64(k)
}

// aggregateByTier groups per-query metrics by tier and computes averages.
// Returns reports in order: easy, medium, hard, overall.
func aggregateByTier(all []queryMetrics) []tierReport {
	byTier := make(map[string][]queryMetrics)
	for _, qm := range all {
		byTier[qm.tier] = append(byTier[qm.tier], qm)
	}

	average := func(metrics []queryMetrics) tierReport {
		if len(metrics) == 0 {
			return tierReport{}
		}
		var tr tierReport
		tr.count = len(metrics)
		for _, m := range metrics {
			tr.mrr += m.recipRank
			tr.recallAt3 += m.recallAt3
			tr.recallAt5 += m.recallAt5
			tr.precisionAt3 += m.precisionAt3
		}
		n := float64(tr.count)
		tr.mrr /= n
		tr.recallAt3 /= n
		tr.recallAt5 /= n
		tr.precisionAt3 /= n
		return tr
	}

	tierOrder := []string{"easy", "medium", "hard"}
	reports := make([]tierReport, 0, 4)
	for _, tier := range tierOrder {
		tr := average(byTier[tier])
		tr.tier = tier
		reports = append(reports, tr)
	}

	overall := average(all)
	overall.tier = "overall"
	reports = append(reports, overall)

	return reports
}

// logReportTable logs the metrics table via t.Logf.
func logReportTable(t *testing.T, method string, reports []tierReport) {
	t.Helper()
	t.Logf("")
	t.Logf("=== %s Retrieval Quality ===", method)
	t.Logf("%-8s | %5s | %5s | %5s | %5s | %s", "Tier", "MRR", "R@3", "R@5", "P@3", "N")
	for _, r := range reports {
		t.Logf("%-8s | %5.2f | %5.2f | %5.2f | %5.2f | %d",
			r.tier, r.mrr, r.recallAt3, r.recallAt5, r.precisionAt3, r.count)
	}
}

// topN returns the first n elements of ids, or all if len < n.
func topN(ids []string, n int) []string {
	if len(ids) <= n {
		return ids
	}
	return ids[:n]
}

// TestRetrievalQuality runs all test cases through Search and FindRelevant,
// computes MRR, Recall@3, Recall@5, and Precision@3 per difficulty tier,
// and logs a summary table. Run with: go test -run TestRetrievalQuality -v
func TestRetrievalQuality(t *testing.T) {
	s := populateBenchStore(t)

	// ── Search ─────────────────────────────────────────────────────────
	t.Run("Search", func(t *testing.T) {
		var all []queryMetrics
		for _, tc := range retrievalTestCases {
			results, err := s.Search(tc.query, "", "")
			if err != nil {
				t.Errorf("Search(%q): %v", tc.query, err)
				continue
			}
			ids := make([]string, len(results))
			for i, m := range results {
				ids[i] = m.Chunk.ID
			}
			qm := computeQueryMetrics(tc, ids)
			all = append(all, qm)

			// Log misses for debugging.
			if qm.recipRank == 0 {
				t.Logf("MISS [%s] query=%q expected=%v got_top3=%v",
					tc.tier, tc.query, tc.relevant, topN(ids, 3))
			}
		}

		reports := aggregateByTier(all)
		logReportTable(t, "Search", reports)
	})

	// ── FindRelevant ───────────────────────────────────────────────────
	t.Run("FindRelevant", func(t *testing.T) {
		var all []queryMetrics
		for _, tc := range retrievalTestCases {
			// Use limit=10 so Recall@5 is meaningful.
			results := s.FindRelevant(tc.query, 10, "", "")
			ids := make([]string, len(results))
			for i, m := range results {
				ids[i] = m.ID
			}
			qm := computeQueryMetrics(tc, ids)
			all = append(all, qm)

			if qm.recipRank == 0 {
				t.Logf("MISS [%s] query=%q expected=%v got_top3=%v",
					tc.tier, tc.query, tc.relevant, topN(ids, 3))
			}
		}

		reports := aggregateByTier(all)
		logReportTable(t, "FindRelevant", reports)
	})
}

// ═══════════════════════════════════════════════════════════════════════
// Latency benchmarks: Search and FindRelevant at various store sizes
// ═══════════════════════════════════════════════════════════════════════

// benchLatencyQueries is a representative set of queries used in latency
// benchmarks. They vary in length and specificity to simulate realistic
// usage patterns: short keyword lookups, natural-language questions, and
// multi-term technical queries.
var benchLatencyQueries = [...]string{
	"database connection pooling configuration",
	"how does authentication work",
	"error handling best practices in Go",
	"deploy to production environment",
	"what version of the API is currently live",
	"Redis cache invalidation strategy",
	"who maintains the infrastructure and Kubernetes cluster",
	"logging and monitoring setup with Grafana",
	"TLS certificate renewal process",
	"microservices communication patterns using gRPC",
}

// generateMemoryText creates a deterministic, realistic memory text from an
// index. It combines four independent vocabulary pools (team, service, domain,
// action) using a division-based index scheme so that at N=10000 every memory
// gets a unique (team, service, domain) triplet. This keeps inter-document
// TF-IDF similarity low and avoids quadratic edge creation during Add(),
// making store setup feasible even at large N.
//
// Combinatorial capacity: 5 × 20 × 25 × 20 × 20 = 10,000,000 unique texts.
func generateMemoryText(idx int) string {
	prefixes := [...]string{
		"#fact", "#decision", "#note", "#status", "#principle",
	}

	teams := [...]string{
		"alpha", "beta", "gamma", "delta", "epsilon",
		"zeta", "eta", "theta", "iota", "kappa",
		"lambda", "mu", "nu", "xi", "omicron",
		"pi", "rho", "sigma", "tau", "upsilon",
	}

	services := [...]string{
		"gateway", "proxy", "worker", "handler", "pipeline",
		"engine", "indexer", "resolver", "controller", "dispatcher",
		"scheduler", "processor", "validator", "transformer", "analyzer",
		"aggregator", "serializer", "encoder", "monitor", "collector",
		"daemon", "broker", "adapter", "connector", "executor",
	}

	domains := [...]string{
		"authentication", "authorization", "billing", "caching", "compliance",
		"cryptography", "deployment", "discovery", "encryption", "fulfillment",
		"geolocation", "healthcheck", "inventory", "journaling", "localization",
		"messaging", "networking", "orchestration", "provisioning", "telemetry",
	}

	actions := [...]string{
		"processes incoming requests through configurable middleware chain",
		"maintains LRU cache with configurable eviction policies",
		"publishes structured events to message bus for downstream consumers",
		"validates input schemas against OpenAPI specification before routing",
		"implements circuit breaker logic with configurable failure thresholds",
		"generates audit trail entries for every state-changing operation",
		"applies rate limiting using sliding window counter algorithm",
		"manages certificate rotation with automatic renewal before expiry",
		"performs incremental backups every six hours with recovery support",
		"routes traffic using consistent hashing for sticky session affinity",
		"enforces access control policies based on claims extracted at edge",
		"collects latency histograms and exports them via telemetry SDK",
		"synchronizes state with peer replicas using conflict-free data types",
		"batches outbound notifications to reduce external API call volume",
		"compresses archived data with zstd before writing to cold storage",
		"deduplicates events using bloom filter with configurable error rate",
		"schedules recurring jobs via distributed cron with leader election",
		"normalizes user input across multiple character encodings and locales",
		"streams change data capture events from database transaction log",
		"marshals protocol buffer messages for internal service calls",
	}

	nt := len(teams)
	nsvc := len(services)
	nd := len(domains)

	prefix := prefixes[idx%len(prefixes)]
	team := teams[idx%nt]
	service := services[(idx/nt)%nsvc]
	domain := domains[(idx/(nt*nsvc))%nd]
	action := actions[(idx/(nt*nsvc*nd))%len(actions)]

	return fmt.Sprintf("%s The %s team's %s %s %s. Reference: entry-%d.",
		prefix, team, domain, service, action, idx)
}

// generateBenchStore creates a MemoryStore populated with n generated memories.
// Content is deterministic so benchmark results are reproducible across runs.
//
// To avoid the O(n²) edge computation in Add() (which makes N=10000 infeasible
// as benchmark setup), this function directly populates the store's internal
// structures: chunks, TF-IDF vectors, and token indices. Similarity edges are
// not computed because the latency benchmarks only exercise the Search and
// FindRelevant read paths, which use TF-IDF vectors and token indices — not edges.
func generateBenchStore(tb testing.TB, n int) *MemoryStore {
	tb.Helper()
	s, err := NewMemoryStore(MemoryStoreOptions{})
	if err != nil {
		tb.Fatalf("NewMemoryStore: %v", err)
	}

	memTypes := [...]string{"fact", "decision", "note", "status"}
	now := time.Now()

	s.mu.Lock()
	for i := 0; i < n; i++ {
		text := generateMemoryText(i)
		vec, norm := vectorize(text)
		tokens := tokenSet(text)
		docLen := docLenFromVector(vec)
		id := fmt.Sprintf("m%d", i+1)

		sc := &storedChunk{
			MemoryChunk: MemoryChunk{
				ID:        id,
				Text:      text,
				Label:     fmt.Sprintf("gen-%d", i),
				Type:      memTypes[i%len(memTypes)],
				CreatedAt: now,
			},
			vector: vec,
			norm:   norm,
			tokens: tokens,
			docLen: docLen,
			edges:  make(map[string]float64),
		}

		s.chunks[id] = sc
		s.indexChunkLocked(id, tokens)
		s.totalDocs++
		s.totalDocLen += docLen
	}
	s.nextID = uint64(n)
	s.mu.Unlock()

	return s
}

// BenchmarkRetrievalSearch measures Search latency at store sizes of 100, 1000,
// and 10000. Each iteration runs a single Search call, cycling through the
// representative query set. Setup (store population) is excluded from timing.
//
// Run with: go test -bench BenchmarkRetrievalSearch -benchmem
func BenchmarkRetrievalSearch(b *testing.B) {
	for _, size := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("N=%d", size), func(b *testing.B) {
			s := generateBenchStore(b, size)
			nq := len(benchLatencyQueries)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = s.Search(benchLatencyQueries[i%nq], "", "")
			}
		})
	}
}

// BenchmarkRetrievalFindRelevant measures FindRelevant latency at store sizes of
// 100, 1000, and 10000. Each iteration runs a single FindRelevant call with
// limit=5, cycling through the representative query set. Setup is excluded.
//
// Run with: go test -bench BenchmarkRetrievalFindRelevant -benchmem
func BenchmarkRetrievalFindRelevant(b *testing.B) {
	for _, size := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("N=%d", size), func(b *testing.B) {
			s := generateBenchStore(b, size)
			nq := len(benchLatencyQueries)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = s.FindRelevant(benchLatencyQueries[i%nq], 5, "", "")
			}
		})
	}
}
