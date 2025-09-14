**Data Flow Diagram**
- A data flow diagram (DFD) is a graphical representation of the "flow" of data through an information system, modeling its process aspects.

## Graphical Representation of Data Flow

```mermaid 
sequenceDiagram
    participant GitHub
    participant Webhook as Webhook Receiver
    participant Fetcher as Code Fetcher
    participant Engine as Review Engine
    participant Analyzers as Analysis Modules
    participant Aggregator as Result Aggregator
    participant Handler as Response Handler
    
    GitHub->>Webhook: Pull Request Event
    Webhook->>Fetcher: Process Webhook
    Fetcher->>Fetcher: Clone Repository
    Fetcher->>Fetcher: Extract Changed Files
    Fetcher->>Engine: Files to Analyze
    
    Engine->>Analyzers: Dispatch Files to Analyzers
    
    par Go Analysis
        Analyzers->>Analyzers: Run golangci-lint
    and AI Analysis
        Analyzers->>Analyzers: Run Gemini AI
    end
    
    Analyzers->>Aggregator: Collect All Issues
    Aggregator->>Aggregator: Remove Duplicates
    Aggregator->>Aggregator: Sort By Severity
    Aggregator->>Handler: Format Results
    Handler->>GitHub: Post Review Comments
```

## Markdown Representation of Data Flow

```markdown
┌──────────┐                       ┌───────────┐
│  GitHub  │                       │  Gollora  │
│          │                       │  Server   │
└──────────┘                       └───────────┘
     │                                   │
     │ 1. Pull Request Created/Updated   │
     │ ───────────────────────────────▶ │
     │                                   │
     │                                   │ 2. Clone Repository
     │                                   │    & Fetch Changes
     │                                   │
     │                                   │ 3. Run Analysis Tools
     │                                   │    (Go Linters, AI)
     │                                   │
     │                                   │ 4. Aggregate Results
     │                                   │
     │ 5. Post Comments                  │
     │ ◀───────────────────────────────  │
     │-----------------------------------│

```

**Data Flow Steps**
1. GitHub triggers a webhook event when a pull request is created or updated.
2. Gollora server clones the repository and fetches the latest changes.
3. Analysis tools are executed on the codebase (e.g., Go linters, AI-powered engine).
4. Results from different analysis modules are aggregated and prioritized.
5. Comments are posted back to GitHub as feedback on the pull request.
