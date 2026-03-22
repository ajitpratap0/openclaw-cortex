// Package locomo provides a synthetic LoCoMo-style benchmark dataset with 10
// QA pairs across 3 multi-session conversations.
//
// LoCoMo (Long Conversational Memory) tests single-hop, multi-hop, and temporal
// retrieval from rich, multi-turn conversation histories.
package locomo

// ConvTurn represents a single turn in a conversation (user + assistant).
type ConvTurn struct {
	User      string
	Assistant string
}

// QAPair is a LoCoMo-style evaluation unit consisting of a conversation to
// ingest and a question/answer pair to evaluate against recalled memories.
type QAPair struct {
	ID           string
	Conversation []ConvTurn // turns to ingest before querying
	Question     string
	GroundTruth  string
	Category     string // "single-hop" | "multi-hop" | "temporal"
}

// Dataset returns the synthetic LoCoMo QA pairs.
//
// The conversations simulate realistic multi-session AI assistant exchanges
// touching careers, project decisions, and personal preferences — the domains
// most common in the published LoCoMo benchmark.
func Dataset() []QAPair {
	return []QAPair{
		// ── Conversation A: Alice the software engineer ───────────────────────
		{
			ID: "locomo-A1",
			Conversation: []ConvTurn{
				{
					User:      "I've been using Python for most of my data work, but lately I've been falling in love with Go.",
					Assistant: "Go is great for performance-critical services. What kind of data work are you doing?",
				},
				{
					User:      "Building high-throughput log ingestion pipelines. Go's concurrency model is a perfect fit.",
					Assistant: "Goroutines and channels make that kind of work elegant. Are you planning to migrate existing Python code?",
				},
			},
			Question:    "What is Alice's favorite programming language?",
			GroundTruth: "Go",
			Category:    "single-hop",
		},
		{
			ID: "locomo-A2",
			Conversation: []ConvTurn{
				{
					User:      "The log ingestion project I mentioned — we've decided to use Go as the primary implementation language.",
					Assistant: "That's a solid choice for your concurrency requirements. Do you have a timeline?",
				},
				{
					User:      "We're targeting a Q3 launch. The team is excited about it.",
					Assistant: "Go's fast compile times should help with your Q3 timeline.",
				},
			},
			Question:    "What language does Alice use for the log ingestion project?",
			GroundTruth: "Go",
			Category:    "multi-hop",
		},
		{
			ID: "locomo-A3",
			Conversation: []ConvTurn{
				{
					User:      "Before I joined my current team I was a data analyst at a fintech startup.",
					Assistant: "That's quite a pivot — from data analysis to building infrastructure. What prompted the change?",
				},
				{
					User:      "I wanted more impact on system design decisions, and SWE roles offered that.",
					Assistant: "That makes sense. Building systems gives you end-to-end ownership.",
				},
			},
			Question:    "Before Alice switched to software engineering, what was her role?",
			GroundTruth: "data analyst",
			Category:    "temporal",
		},

		// ── Conversation B: Bob the infrastructure lead ───────────────────────
		{
			ID: "locomo-B1",
			Conversation: []ConvTurn{
				{
					User:      "We're evaluating Kubernetes versus Nomad for our container orchestration.",
					Assistant: "Both are solid choices. What does your team's operational experience look like?",
				},
				{
					User:      "The team knows Kubernetes well. We've been running it in staging for six months.",
					Assistant: "With six months of staging experience, Kubernetes is the lower-risk choice for you.",
				},
			},
			Question:    "What container orchestration platform is Bob's team experienced with?",
			GroundTruth: "Kubernetes",
			Category:    "single-hop",
		},
		{
			ID: "locomo-B2",
			Conversation: []ConvTurn{
				{
					User:      "We decided to go with Kubernetes for production. The migration starts next month.",
					Assistant: "Great! Do you have a rollback plan for the first production rollout?",
				},
				{
					User:      "Yes — blue-green deployments initially, then we'll switch to canary releases.",
					Assistant: "Blue-green is safer for the first migration. Canary can come once you're confident in the pipeline.",
				},
			},
			Question:    "What deployment strategy will Bob's team use initially for Kubernetes?",
			GroundTruth: "blue-green",
			Category:    "multi-hop",
		},
		{
			ID: "locomo-B3",
			Conversation: []ConvTurn{
				{
					User:      "Last year we were running bare-metal servers with Ansible for configuration management.",
					Assistant: "Ansible is well-loved for bare-metal. Moving to Kubernetes is a big shift — how is the team adapting?",
				},
				{
					User:      "Slowly but surely. The engineers who were heavy Ansible users are now becoming Helm chart authors.",
					Assistant: "That's a natural evolution. Helm charts give you the same declarative mindset.",
				},
			},
			Question:    "Before Kubernetes, what did Bob's team use for configuration management?",
			GroundTruth: "Ansible",
			Category:    "temporal",
		},

		// ── Conversation C: Carol the ML engineer ─────────────────────────────
		{
			ID: "locomo-C1",
			Conversation: []ConvTurn{
				{
					User:      "My go-to framework for training deep learning models is PyTorch.",
					Assistant: "PyTorch has become dominant for research. Are you using it for production serving too?",
				},
				{
					User:      "For serving I use TorchServe or export to ONNX depending on latency requirements.",
					Assistant: "ONNX gives you runtime flexibility across different inference engines — good strategy.",
				},
			},
			Question:    "What framework does Carol use to train deep learning models?",
			GroundTruth: "PyTorch",
			Category:    "single-hop",
		},
		{
			ID: "locomo-C2",
			Conversation: []ConvTurn{
				{
					User:      "I'm now running my PyTorch training jobs on a cluster with 8 A100 GPUs.",
					Assistant: "Eight A100s is serious compute. What's your typical training time for a large model?",
				},
				{
					User:      "For our largest NLP model, about 12 hours per epoch.",
					Assistant: "That's efficient for A100s. Are you using gradient checkpointing to save memory?",
				},
				{
					User:      "Yes, combined with mixed-precision training via torch.cuda.amp.",
					Assistant: "Mixed-precision plus gradient checkpointing is the standard recipe for large model training.",
				},
			},
			Question:    "What hardware does Carol use for training PyTorch models?",
			GroundTruth: "A100 GPUs",
			Category:    "multi-hop",
		},
		{
			ID: "locomo-C3",
			Conversation: []ConvTurn{
				{
					User:      "I started my ML career working with TensorFlow 1.x before TF2 and PyTorch took over.",
					Assistant: "TF1's session-based graph approach was quite different. Do you miss anything from it?",
				},
				{
					User:      "Not really — eager execution in PyTorch is so much more intuitive for debugging.",
					Assistant: "Debugging with eager mode is a night-and-day difference from static graphs.",
				},
			},
			Question:    "What ML framework did Carol use before PyTorch?",
			GroundTruth: "TensorFlow",
			Category:    "temporal",
		},

		// ── Mixed: cross-conversation temporal ───────────────────────────────
		{
			ID: "locomo-D1",
			Conversation: []ConvTurn{
				{
					User:      "My team stores all our feature vectors in Redis for fast lookup during inference.",
					Assistant: "Redis is popular for feature stores. Are you using the native vector module or a separate vector DB?",
				},
				{
					User:      "Currently native Redis with RediSearch, but we're evaluating Pinecone.",
					Assistant: "Pinecone handles very large vector workloads better at scale. Benchmark latency before deciding.",
				},
				{
					User:      "We eventually decided to migrate to Pinecone after the benchmark confirmed 3x lower p99 latency.",
					Assistant: "That's a meaningful improvement. Pinecone's indexing handles billion-scale vectors well.",
				},
			},
			Question:    "What did the team initially use for feature vector storage before switching?",
			GroundTruth: "Redis",
			Category:    "temporal",
		},
	}
}
