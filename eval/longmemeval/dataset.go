// Package longmemeval provides a synthetic LongMemEval-style benchmark dataset
// with 10 QA pairs testing temporal reasoning, multi-hop retrieval, and
// knowledge-update (superseded memory) scenarios.
//
// LongMemEval specifically probes an agent's ability to recall information from
// long-horizon conversation histories, including cases where facts have changed
// over time.
package longmemeval

// MemoryFact is a pre-formed statement that gets ingested directly via Store
// (rather than a full conversation turn) to simulate a long conversation history.
type MemoryFact struct {
	Content string
	// DatasetValidFrom and DatasetValidTo are dataset metadata for human
	// readability only. The "Dataset" prefix is intentional: these fields are
	// NOT forwarded to the openclaw-cortex binary — the harness calls
	// client.Store(ctx, fact.Content) and ignores them entirely.
	// Temporal-versioning paths (valid_from/valid_to in the store, --supersedes,
	// SearchFilters.AsOf) are therefore out of scope for this harness; it measures
	// semantic retrieval only. See longmemeval/harness.go for the full rationale.
	DatasetValidFrom string // e.g. "2024-01" — dataset documentation only; NOT passed to binary
	DatasetValidTo   string // non-empty = superseded fact; NOT passed as --supersedes
}

// QAPair is a LongMemEval-style evaluation unit.
type QAPair struct {
	ID          string
	Facts       []MemoryFact // facts to ingest (in order) before querying
	Question    string
	GroundTruth string
	Category    string // "temporal" | "multi-hop" | "knowledge-update"
}

// Dataset returns the synthetic LongMemEval QA pairs.
func Dataset() []QAPair {
	return []QAPair{
		// ── Temporal: retrieve the most recent fact ───────────────────────────
		{
			ID: "lme-T1",
			Facts: []MemoryFact{
				{Content: "As of January 2024, Diana's job title is Software Engineer at Acme Corp.", DatasetValidFrom: "2024-01"},
				{Content: "As of June 2024, Diana's job title is Senior Software Engineer at Acme Corp.", DatasetValidFrom: "2024-06"},
			},
			Question:    "What is Diana's current job title?",
			GroundTruth: "Senior Software Engineer",
			Category:    "temporal",
		},
		{
			ID: "lme-T2",
			Facts: []MemoryFact{
				{Content: "In March 2024, the team's primary database was MySQL running on-premises.", DatasetValidFrom: "2024-03"},
				{Content: "In September 2024, the team migrated their primary database from MySQL to PostgreSQL on AWS RDS.", DatasetValidFrom: "2024-09"},
			},
			Question:    "What database does the team use after September 2024?",
			GroundTruth: "PostgreSQL",
			Category:    "temporal",
		},
		{
			ID: "lme-T3",
			Facts: []MemoryFact{
				{Content: "Before April 2024, Ethan worked remotely from Seattle, Washington.", DatasetValidFrom: "2023-01", DatasetValidTo: "2024-04"},
				{Content: "From April 2024 onwards, Ethan relocated to Austin, Texas and works in a hybrid model.", DatasetValidFrom: "2024-04"},
			},
			Question:    "Where does Ethan work from after April 2024?",
			GroundTruth: "Austin, Texas",
			Category:    "temporal",
		},

		// ── Multi-hop: chain facts to answer ─────────────────────────────────
		{
			ID: "lme-M1",
			Facts: []MemoryFact{
				{Content: "Fiona is the lead engineer on Project Atlas.", DatasetValidFrom: "2024-01"},
				{Content: "Project Atlas uses Rust as its primary programming language.", DatasetValidFrom: "2024-01"},
				{Content: "Fiona has 8 years of experience in systems programming.", DatasetValidFrom: "2024-01"},
			},
			Question:    "What language does the Project Atlas lead engineer use?",
			GroundTruth: "Rust",
			Category:    "multi-hop",
		},
		{
			ID: "lme-M2",
			Facts: []MemoryFact{
				{Content: "George manages the Berlin office team.", DatasetValidFrom: "2024-02"},
				{Content: "The Berlin office team deploys to Google Cloud Platform (GCP).", DatasetValidFrom: "2024-02"},
				{Content: "GCP deployments in the Berlin office use Cloud Run for containerised services.", DatasetValidFrom: "2024-02"},
			},
			Question:    "What cloud service does George's team use for containerised deployments?",
			GroundTruth: "Cloud Run",
			Category:    "multi-hop",
		},
		{
			ID: "lme-M3",
			Facts: []MemoryFact{
				{Content: "Hannah leads the recommendation engine team.", DatasetValidFrom: "2024-03"},
				{Content: "The recommendation engine team's model is trained on user click-through data.", DatasetValidFrom: "2024-03"},
				{Content: "Click-through data is stored in BigQuery and refreshed daily.", DatasetValidFrom: "2024-03"},
			},
			Question:    "Where is the training data for Hannah's team's model stored?",
			GroundTruth: "BigQuery",
			Category:    "multi-hop",
		},

		// ── Knowledge-update: answer with the superseded (old) fact ──────────
		{
			ID: "lme-K1",
			Facts: []MemoryFact{
				{Content: "Ivan's team meeting cadence was weekly every Monday (valid until Q2 2024).", DatasetValidFrom: "2023-01", DatasetValidTo: "2024-06"},
				{Content: "Starting Q3 2024, Ivan's team switched their meeting cadence to bi-weekly every other Monday.", DatasetValidFrom: "2024-07"},
			},
			Question:    "What was Ivan's original team meeting cadence before Q3 2024?",
			GroundTruth: "weekly",
			Category:    "knowledge-update",
		},
		{
			ID: "lme-K2",
			Facts: []MemoryFact{
				{Content: "Before October 2024, Julia's team used Jenkins for CI/CD pipelines.", DatasetValidFrom: "2022-01", DatasetValidTo: "2024-10"},
				{Content: "From October 2024, Julia's team migrated CI/CD from Jenkins to GitHub Actions.", DatasetValidFrom: "2024-10"},
			},
			Question:    "What CI/CD tool did Julia's team use before October 2024?",
			GroundTruth: "Jenkins",
			Category:    "knowledge-update",
		},
		{
			ID: "lme-K3",
			Facts: []MemoryFact{
				{Content: "Until mid-2024, Kevin's microservices communicated over REST/HTTP.", DatasetValidFrom: "2022-01", DatasetValidTo: "2024-06"},
				{Content: "From mid-2024, Kevin's team adopted gRPC for inter-service communication, replacing REST.", DatasetValidFrom: "2024-06"},
			},
			Question:    "What protocol did Kevin's services originally use for communication?",
			GroundTruth: "REST",
			Category:    "knowledge-update",
		},

		// ── Additional temporal chain ─────────────────────────────────────────
		{
			ID: "lme-T4",
			Facts: []MemoryFact{
				{Content: "Laura's ML model version 1.0 achieved 82% accuracy on the test set in January 2024.", DatasetValidFrom: "2024-01"},
				{Content: "Laura's ML model version 2.0 achieved 89% accuracy on the test set in August 2024, superseding v1.0.", DatasetValidFrom: "2024-08"},
			},
			Question:    "What accuracy did Laura's latest ML model version achieve?",
			GroundTruth: "89%",
			Category:    "temporal",
		},
	}
}
