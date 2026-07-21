# OpenAI Plugins Directory submission — BeforeDone v1.0.0

## Distribution boundary

Submit **Skills only**. BeforeDone v1 has no MCP server. The public-directory
bundle contains `verify-before-done` and `investigate-agent-incident`; it does
not contain Codex lifecycle hooks and cannot enforce the Stop Gate. The full
Hook-enabled Codex Plugin remains available from the public Git marketplace.

## Listing

- **Name:** BeforeDone
- **Short description:** Verify agent work and investigate failed runs.
- **Category:** Developer Tools
- **Website:** https://rrrrrredy.github.io/beforedone/
- **Support:** https://github.com/rrrrrredy/beforedone/issues
- **Privacy:** https://rrrrrredy.github.io/beforedone/privacy.html
- **Terms:** https://rrrrrredy.github.io/beforedone/terms.html
- **Source:** https://github.com/rrrrrredy/beforedone
- **Logo:** `media/product-hunt-thumbnail.png` (240×240 RGB PNG)
- **Bundle:**
  [beforedone-openai-directory-skills-v1.0.0.zip](https://github.com/rrrrrredy/beforedone/releases/download/v1.0.0/beforedone-openai-directory-skills-v1.0.0.zip)
- **Bundle SHA-256:** `4f441aca93655af97a38e1d2c1516a237a22ebd16cc906065e2c36badd265cd8`

Long description:

> BeforeDone helps developers verify a coding agent's completion claim against
> fresh local checks and investigate failed runs from observable evidence. Its
> two skills guide the open-source BeforeDone CLI: one creates and evaluates
> evidence receipts bound to relevant files; the other builds a local Incident
> Report, identifies the earliest evidence-supported divergence, and prepares
> safe replay analysis. The Skills-only Directory edition is a manual workflow
> and does not install or claim Codex Stop Hook enforcement. No account, cloud
> backend, or BeforeDone telemetry is required.

## Starter prompts

1. `Use BeforeDone to verify this task before I accept the completion claim.`
2. `Investigate this failed coding-agent run and show the first observable divergence.`
3. `Check whether the latest BeforeDone receipt is still fresh after these edits.`
4. `Analyze the latest replay case without executing any verifier commands.`

## Release notes

Initial v1.0.0 submission. This Skills-only plugin packages two audited manual
workflows backed by the Apache-2.0 BeforeDone CLI. It does not include an MCP
server or automatic Stop Hook. The separate Git Marketplace distribution adds
Codex lifecycle hooks for users who explicitly install and trust them.

## Human checkpoints

The publisher must complete these steps in the OpenAI Platform:

1. Open the [submission portal](https://platform.openai.com/plugins) and select
   the organization with Apps Management write permission.
2. Complete and select the verified individual developer identity.
3. Upload the versioned Skills-only ZIP and production logo. Confirm the logo
   is sharp in the portal preview; use a higher-resolution brand-identical
   square export if the portal requests one.
4. Enter the five positive and three negative cases from `test-cases.md`.
5. Select only countries or regions where the publisher is ready to support the product.
6. Review the final draft, submit it for review, and publish only after approval.

Do not upload the Git Marketplace plugin directory to the Skills-only form: it
contains hooks and wrapper scripts outside the submitted distribution contract.

The current official requirements are maintained in
[Submit plugins](https://learn.chatgpt.com/docs/submit-plugins); the portal is
authoritative if its validation rules change after this checklist was prepared.
