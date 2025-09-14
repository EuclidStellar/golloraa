## Logical Flow of Files in Gollora

```mermaid 
flowchart TD
    subgraph "Configuration"
        A1[config.yaml]
        A2[analysis_tools.yaml]
        A3[Environment Variables]
    end
    
    subgraph "Core Components"
        B[main.go]
        C[webhook.go]
        D[fetcher.go]
        E[review_engine.go]
        F[aggregator.go]
        G[response_handler.go]
    end
    
    subgraph "Analysis & Agentic Modules"
        H[golang.go]
        I[ai_analyzer.go]
        P[agent.go]
        Q[ast_tool.go]
    end
    
    subgraph "Models"
        J[types.go]
        K[result.go]
    end
    
    subgraph "External Tools"
        L[golangci-lint]
        M[staticcheck]
        N[gosec]
        O[Gemini API]
    end
    
    A1 --> B
    A2 --> B
    A3 --> B
    
    B --> C
    B --> E
    B --> P
    
    C --> D
    D --> E
    E --> H
    E --> I
    H --> F
    I --> F
    F --> G
    
    P --> Q
    P --> O
    
    H --> J
    H --> K
    I --> J
    I --> K
    
    H --> L
    H --> M
    H --> N
    I --> O

```

### Webhook Processing Flow

```mermaid
stateDiagram-v2
    [*] --> WebhookReceived
    
    WebhookReceived --> ValidateSignature
    ValidateSignature --> ExtractPayload
    ValidateSignature --> InvalidSignature
    InvalidSignature --> [*]
    
    ExtractPayload --> DeterminePRAction
    DeterminePRAction --> ProcessPR: opened/synchronized
    DeterminePRAction --> IgnoreEvent: closed/other
    IgnoreEvent --> [*]
    
    ProcessPR --> CloneRepository
    CloneRepository --> GetChangedFiles
    GetChangedFiles --> InitiateAnalysis
    InitiateAnalysis --> [*]
```

### Analysis Flow

```mermaid

stateDiagram-v2
    [*] --> ReceiveFiles
    
    ReceiveFiles --> DetermineLanguages
    DetermineLanguages --> DispatchAnalyzers
    
    DispatchAnalyzers --> StaticAnalysis: .go, .py files
    DispatchAnalyzers --> AIAnalysis: All files
    
    StaticAnalysis --> RunLinters
    RunLinters --> CollectStaticIssues
    
    AIAnalysis --> PrepareAIPrompt
    PrepareAIPrompt --> CallGeminiAPI
    CallGeminiAPI --> ParseAIResponse
    ParseAIResponse --> CollectAIIssues
    
    CollectStaticIssues --> AggregateResults
    CollectAIIssues --> AggregateResults
    
    AggregateResults --> AISeverityScoring
    AISeverityScoring --> RemoveDuplicates
    RemoveDuplicates --> AnalyzeDependencies
    AnalyzeDependencies --> SortByPriority
    SortByPriority --> FormatForOutput
    FormatForOutput --> PostComments
    
    PostComments --> [*]

```

### Interactive Q&A Flow

```mermaid
flowchart TD
    subgraph "Q&A Agent Logic"
        A[User Question] --> B{AI Router};
        B -- "Specific Go Query" --> C[AST Tool];
        B -- "General Question" --> D[Vector Search (RAG)];
        
        C --> E[Parse Go AST];
        E --> F[Extract Structural Info];
        
        D --> G[Generate Embedding];
        G --> H[Find Similar Chunks];
        H --> I[Build Context];
        
        F --> J{Format Response};
        I --> J;
        
        J --> K[Final Answer];
    end
```

### Go Analysis Flow 

```mermaid
flowchart TD
    A[Receive Go Files] --> B{Is go.mod present?}
    B -->|Yes| D[Run golangci-lint]
    B -->|No| C[Initialize temporary Go module]
    C --> D
    
    D --> E[Parse golangci-lint Output]
    E --> F[Map Issues to Standard Format]
    F --> G[Return Standardized Issues]
   

```

### AI Analysis Flow

```mermaid
flowchart TD
    A[Receive File Content] --> B[Prepare AI Prompt]
    B --> C[Call Gemini API]
    C --> D{Valid JSON Response?}
    
    D -->|Yes| F[Parse JSON]
    D -->|No| E[Clean and Extract JSON]
    E --> F
    
    F --> G{Complete JSON?}
    G -->|Yes| I[Convert to Issues]
    G -->|No| H[Recover Partial JSON]
    H --> I
    
    I --> J[Return AI Issues]

```
