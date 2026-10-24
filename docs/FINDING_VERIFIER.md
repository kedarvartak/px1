# Finding verifier

Every model-proposed finding passes through a separate bounded verifier call.
Candidates without a valid line range and evidence are dropped before model
transfer. The verifier may only assess supplied candidates, returns strict JSON,
and must account for every ID. Unsupported findings and findings below 0.6
confidence are dropped with a recorded reason; the verifier cannot create new
findings or approve a pull request.
