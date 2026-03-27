# Create Domain Knowledge File

This guide outlines best practices for creating domain knowledge files in the `.github/.copilot/domain_knowledge/` folder. These files serve as a source of truth for understanding the Istio architecture, patterns, and implementation details.

## Naming Convention

- Use snake_case for filenames: `component_name.md`
- Use descriptive names that clearly indicate the content: `pilot_push_context.md`, `istioctl_commands.md`, etc.

## Structure

Each domain knowledge file should follow this structure:

1. **Title**: Clear, descriptive title at the top using H1 (`# Title`)
2. **Introduction**: Brief overview of what the document explains
3. **Conceptual Overview**: High-level explanation of the concept or component
4. **Implementation Architecture**: Key components and their relationships
5. **Code Implementation**: Relevant code patterns with examples
6. **Key Interfaces/Models**: Important data structures, interfaces, or protocols
7. **Example Use Cases**: Real-world examples of how the component is used
8. **Best Practices**: Guidelines for working with this component
9. **Related Components**: Links to other domain knowledge files that relate to this one

## Content Guidelines

### 1. Technical Depth

- Balance high-level concepts with implementation details
- Include enough information for new developers to understand the system
- Reference specific file paths for key implementations
- Show actual code examples, not just descriptions

### 2. Code Examples

- Include small, focused code snippets that illustrate key patterns
- Reference file paths above each code example
- Use Go syntax highlighting for Go code examples
- Keep examples concise, focusing on the most important patterns

Example:
```go
// From pilot/pkg/config/kube/gateway/deploymentcontroller.go
func (d *DeploymentController) Run(stop <-chan struct{}) {
	kube.WaitForCacheSync(
		"deployment controller",
		stop,
		d.namespaces.HasSynced,
    )
    // blocks indefinitely until stop is closed
}
```

### 3. Component Relationships

- Clearly explain how components interact with each other
- Use numbered lists or flow descriptions to show sequences
- Explain communication patterns between services

### 4. File References

- Include complete file paths to make it easy to find implementations
- Group related files by functionality or component
- List both interface/contract files and implementations

Example:
```
Istio Revision Tags:
- Creation (via Helm): manifests/charts/istio-control/istio-discovery/templates/revision-tags.yaml
- Creation (via Istioctl): istioctl/pkg/tag/tag.go
- Detection: pkg/revisions/tag_watcher.Go```

### 5. Best Practices

- Include best practices specific to the component
- Highlight common pitfalls and how to avoid them
- Show code examples of proper usage patterns

## Required Sections for Common Component Types

### API Components

- Request/Response flow
- Validation patterns
- Authorization checks
- Error handling
- Example API calls

### Asynchronous Operations

- Operation flow from request to completion
- Queue/messaging patterns
- Status tracking
- Error handling and recovery
- Example of complete operation cycle

### Data Models

- Key fields and their meaning
- Validation rules
- Relationships with other models
- Proto definitions if applicable
- Example model instances

### Services

- Service responsibilities
- Dependencies
- Configuration
- Deployment patterns
- Key methods/endpoints

## Example Domain Knowledge File Structure

```markdown
# Component Name

Brief introduction explaining the purpose and importance of this component.

## Overview

High-level conceptual explanation with key points.

## Architecture

How this component fits into the overall system, with diagrams if helpful.

## Implementation Details

### Key Patterns

Important implementation patterns with code examples.

### Component Flow

Step-by-step explanation of how data/requests flow through the component.

## Key Files

List of critical files with their purposes.

## Examples

Real-world examples showing the component in action.

## Best Practices

Guidelines for working with this component effectively.

## Related Components

Links to related domain knowledge files.
```

## Maintenance

- Update domain knowledge files when significant changes occur to the described components
- Keep code examples current with the latest implementation patterns
- Add new domain knowledge files when new components are added

