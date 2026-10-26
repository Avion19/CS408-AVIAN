# Checkpoint 3: member profiles and permissions

Checkpoint 3 implements public member profiles, inline profile editing, and confirmed account deletion. Authenticated ownership checks protect every profile mutation, and account removal revokes all sessions. The implementation follows the approved specification's “Member profiles and permissions” checkpoint.

## Public profiles

`GET /members/:id` displays the member's display name, username, broad region, and bio. The page includes the ten newest public chirps, ordered by creation time and ID, plus flock memberships linked to their viewer routes. Membership cards identify owners and show the flock's topic and region. Profiles without chirps, memberships, or a bio have clear empty states.

Profile queries select public member fields rather than account credentials. Email addresses, password hashes, and session details are excluded. Profile chirps are always restricted to public visibility, including when the profile owner is viewing the page. Go's HTML templates escape all member-supplied text. The page uses the existing shared navigation, field-journal palette, and responsive Bootstrap layout.

## Inline editing

The profile owner can open a prefilled editor through `?edit=1` on the profile page. The form updates username, display name, region, and bio through `POST /members/:id`. Saving redirects to the same profile; Cancel returns to its display mode without writing data. The session reloads member information from SQLite on each request, so saved identity changes also appear in the shared header.

Usernames are normalized to lowercase and checked against the registration format and case-insensitive uniqueness constraints. Display names require 1–80 characters; optional regions allow up to 100 characters and optional bios up to 500. Text lengths count Unicode characters, and invalid UTF-8 is rejected. Validation failures retain the editor, entered fields, and an accessible error message. Duplicate usernames return HTTP 409 without partially updating the profile.

Only public profile fields are writable. Submitted account IDs, email addresses, or ownership fields do not change the target account. Neither guests nor other members receive edit or deletion controls, and adding `?edit=1` does not grant access.

## Confirmed account deletion

Account settings expand on the owner's profile to explain the permanent effects of removal. A required confirmation checkbox and a separate POST button submit to `/members/:id/delete`. The server independently requires the authenticated owner, a valid session CSRF token, and `confirm_delete=yes`. Opening a URL with GET never deletes an account.

`Store.DeleteAccount` performs the database changes in one transaction:

1. Resolve the account from the authenticated member's profile.
2. Reject deletion if any flock membership identifies the member as its current owner.
3. Check original-creator references and reassign them to the current owner after a completed transfer. A flock without a replacement owner also blocks deletion.
4. Collect the account's chirp photo filenames.
5. Delete the account. SQLite foreign-key cascades remove its profile, every session, public and flock-only chirps, species-tag relationships, and flock memberships.
6. Commit before removing associated photo files.

Successful deletion clears both authentication and CSRF cookies and redirects to Home. Sessions on other browsers are also invalidated because their database rows no longer exist. Accounts, flocks, and chirps belonging to other members remain intact. File cleanup accepts only flat filenames beneath the configured upload directory; missing files are tolerated, unsafe paths are skipped, and filesystem failures are logged.

A member without flock ownership can delete their account. An account that still owns even one flock receives HTTP 409 with an explanation and retains its profile, data, and sessions. Current membership roles determine ownership, so a successor owner is protected even when another member originally created the flock. Ownership-transfer controls remain part of Checkpoint 5; database fixtures exercise that dependency in this checkpoint's tests.

## Permissions and request handling

| Request | Result |
| --- | --- |
| Public profile read | Public profile fields, public chirps, and memberships |
| Owner opens inline editor | Prefilled form on the profile page |
| Guest submits a mutation with a valid guest CSRF token | HTTP 401 |
| Another signed-in member submits a mutation | HTTP 403 |
| Missing, forged, cross-session, or expired-session CSRF token | HTTP 403 |
| Invalid or missing member ID | HTTP 404 for profile reads and authenticated mutations |
| Invalid profile fields or missing deletion confirmation | HTTP 422 with profile feedback |
| Duplicate username or unresolved flock ownership | HTTP 409 without changing the account |
| Successful profile save | HTTP 303 to the profile |
| Successful account deletion | HTTP 303 to Home with session cookies cleared |

## Implementation and verification

`profiles.go` contains profile queries, account removal, ownership checks, field validation, and HTTP handlers. `templates/profile.html` implements display, inline editing, memberships, public chirps, and the confirmed deletion form. `server.go` registers the protected write routes and uses the existing session and CSRF middleware. `static/css/avian.css` adds the profile layout and responsive behavior. The existing schema already supplies the required foreign keys and uniqueness constraints, so no schema migration is needed.

`profiles_test.go` exercises public display and empty states; prefilled edits and saved values; validation limits and Unicode; duplicate usernames; HTML escaping and SQL input as text; public-only chirps and newest-first limits; owner-only controls; guest, cross-account, invalid-ID, and expired-session rejection; deletion confirmation; dependent-row and photo cleanup; every-session revocation; rejection of current flock owners; creator-reference repair after fixture transfers; rollback after a database failure; safe photo paths; and persistence after closing and reopening SQLite. The existing account suite continues to verify registration, bcrypt, login/logout, hashed session storage, expiration, rotation, and CSRF behavior.
