package dmr

// syntheticDataset returns a small synthetic DMR-style dataset for unit tests
// and CI runs that do not have access to the real DMR dataset file.
//
// The synthetic set covers all five hop depths (1–5) with concise, factual
// conversations that mirror the multi-hop chaining structure of the published
// DMR benchmark.
func syntheticDataset() []QAPair {
	return []QAPair{
		// 1-hop: single direct fact retrieval
		{
			ID: "dmr-syn-1hop-1",
			Conversation: []ConvTurn{
				{Speaker: "human", Content: "My name is Alice and I work at Acme Corp."},
				{Speaker: "ai", Content: "Nice to meet you, Alice! What do you do at Acme Corp?"},
				{Speaker: "human", Content: "I'm a senior software engineer there."},
				{Speaker: "ai", Content: "That sounds like a great role!"},
			},
			Question: "Where does Alice work?",
			Answer:   "Acme Corp",
			Category: "1-hop",
		},
		{
			ID: "dmr-syn-1hop-2",
			Conversation: []ConvTurn{
				{Speaker: "human", Content: "I prefer Python for scripting tasks."},
				{Speaker: "ai", Content: "Python is very versatile for scripting."},
			},
			Question: "What programming language does the user prefer for scripting?",
			Answer:   "Python",
			Category: "1-hop",
		},

		// 2-hop: chain two facts
		{
			ID: "dmr-syn-2hop-1",
			Conversation: []ConvTurn{
				{Speaker: "human", Content: "Bob is the lead engineer on Project Mercury."},
				{Speaker: "ai", Content: "Interesting! What does Project Mercury involve?"},
				{Speaker: "human", Content: "Project Mercury uses Rust as its primary language."},
				{Speaker: "ai", Content: "Rust is a great choice for systems projects."},
			},
			Question: "What programming language does Bob's project use?",
			Answer:   "Rust",
			Category: "2-hop",
		},
		{
			ID: "dmr-syn-2hop-2",
			Conversation: []ConvTurn{
				{Speaker: "human", Content: "Carol manages the Paris office."},
				{Speaker: "ai", Content: "The Paris office is an important hub."},
				{Speaker: "human", Content: "The Paris office deploys to AWS."},
				{Speaker: "ai", Content: "AWS has great European availability zones."},
			},
			Question: "What cloud provider does Carol's office use?",
			Answer:   "AWS",
			Category: "2-hop",
		},

		// 3-hop: chain three facts
		{
			ID: "dmr-syn-3hop-1",
			Conversation: []ConvTurn{
				{Speaker: "human", Content: "David leads the analytics team."},
				{Speaker: "ai", Content: "What does the analytics team work on?"},
				{Speaker: "human", Content: "The analytics team maintains the data warehouse."},
				{Speaker: "ai", Content: "Data warehouses are critical for reporting."},
				{Speaker: "human", Content: "The data warehouse is hosted on BigQuery."},
				{Speaker: "ai", Content: "BigQuery scales well for large datasets."},
			},
			Question: "What platform hosts the data warehouse led by David's team?",
			Answer:   "BigQuery",
			Category: "3-hop",
		},

		// 4-hop: chain four facts
		{
			ID: "dmr-syn-4hop-1",
			Conversation: []ConvTurn{
				{Speaker: "human", Content: "Eve is the CTO of StartupX."},
				{Speaker: "ai", Content: "What does StartupX build?"},
				{Speaker: "human", Content: "StartupX builds the Hermes platform."},
				{Speaker: "ai", Content: "What does Hermes do?"},
				{Speaker: "human", Content: "Hermes is a real-time messaging system."},
				{Speaker: "ai", Content: "What infrastructure does Hermes run on?"},
				{Speaker: "human", Content: "Hermes runs on Kubernetes clusters in GCP."},
				{Speaker: "ai", Content: "GCP and Kubernetes are a common pairing."},
			},
			Question: "What infrastructure does the platform built by StartupX's CTO run on?",
			Answer:   "Kubernetes",
			Category: "4-hop",
		},

		// 5-hop: chain five facts
		{
			ID: "dmr-syn-5hop-1",
			Conversation: []ConvTurn{
				{Speaker: "human", Content: "Frank is the head of engineering at Globo Inc."},
				{Speaker: "ai", Content: "What's the main product at Globo Inc?"},
				{Speaker: "human", Content: "Globo Inc makes the Atlas search engine."},
				{Speaker: "ai", Content: "What database does Atlas use?"},
				{Speaker: "human", Content: "Atlas uses Elasticsearch for its index."},
				{Speaker: "ai", Content: "Who maintains the Elasticsearch cluster?"},
				{Speaker: "human", Content: "The infra team maintains the Elasticsearch cluster."},
				{Speaker: "ai", Content: "Who leads the infra team?"},
				{Speaker: "human", Content: "Grace leads the infra team."},
				{Speaker: "ai", Content: "Grace must be very experienced then."},
			},
			Question: "Who leads the team that maintains the database used by the search engine at Frank's company?",
			Answer:   "Grace",
			Category: "5-hop",
		},
	}
}
