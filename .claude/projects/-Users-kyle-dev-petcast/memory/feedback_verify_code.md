---
name: Verify everything written
description: User expects comprehensive test coverage for all code written, not just unit tests
type: feedback
---

Always fully verify code with tests as it's written. Unit tests alone aren't sufficient — need integration and e2e tests that exercise the real flows. Don't leave gaps where core functionality (like the full pipeline) goes untested.

**Why:** User explicitly called out the gap between having 43 unit tests but no real e2e coverage of the core value (frame → server → pipeline → image).

**How to apply:** When writing new functionality, include tests that exercise the full integration path, not just isolated units. Use mocks only for external services (APIs), not for internal components. If something can't be tested without an API key, mock the external boundary and test everything else real.
