# Finding contract

Model findings use strict JSON with no unknown fields. Every finding names a
repository-relative path and valid line range, P0–P2 severity, supported type,
title, explanation, concrete evidence, optional local suggestion, confidence,
and optional rule ID. Runtime validation mirrors `schemas/finding.schema.json`
and caps both the number and size of findings before persistence or posting.
