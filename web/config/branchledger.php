<?php

return [
    // Base URL of the Go API, reached only from this Laravel process — the
    // browser never talks to it directly (architecture: same-origin gateway).
    'go_api_base_url' => env('GO_API_BASE_URL', 'http://localhost:8099'),

    // Hex-encoded HMAC secret shared with the Go API's DELEGATION_SECRET.
    // Must never reach the browser or logs.
    'delegation_secret' => env('DELEGATION_SECRET'),

    // Short-lived on purpose: a fresh token is signed for every proxied
    // request, so this only needs to cover one request/response round trip
    // plus reasonable clock skew.
    'delegation_ttl_seconds' => (int) env('DELEGATION_TTL_SECONDS', 60),
];
