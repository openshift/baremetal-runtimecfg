---
name: runtimecfg-review
description: Review changes to baremetal-runtimecfg
---

## Step 1: Review the AGENTS.md file for accuracy, completeness, and documentation quality. Specifically check:

1. **Accuracy**: Verify all command-line flags, default values, and usage examples match the actual code in cmd/
2. **Completeness**: Ensure all agents and their features are documented
3. **Code references**: Check that any file paths or line references are current
4. **Consistency**: Verify terminology and formatting is consistent throughout
5. **Clarity**: Suggest improvements to explanations and examples

Read the AGENTS.md file and compare against the source code for each component (corednsmonitor, dnsmasqmonitor, dynkeepalived, monitor, runtimecfg).

Provide specific suggestions for improvements with file references where applicable.

## Step 2: Review code changes in cmd/, pkg/, and hack/. Specifically look for:

1. **Duplication**: New code should not be a near-duplicate of existing code.
2. **Consistency**: New code should be written in the same style as existing code.

