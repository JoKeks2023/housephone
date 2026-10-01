# T-0014: Verwaltung in der iPhone-App (HPHN-37)

Admin-iPhones verwalten die Bridge direkt in der App: nur im Heimnetz bzw. über Tailscale, jede Anfrage zusätzlich mit einem Face-ID-geschützten Admin-Schlüssel signiert. Grundlage: Ticket HPHN-37, Vorgabe „Verwaltung nur im Heimnetz“ (2026-09-29), Freigabe-Schnittstelle aus HPHN-41, Profile aus HPHN-42.

Branch `feat/app-admin` auf `feat/profiles` (PR #39). Entscheidungen: ADR-0009.
