# Checkpoint 4: Species, Feed, and chirps

The approved specification lists:

> Deliver Species browsing/selection with seeded reference photos, source links, and tagged visible chirps; paginated Feed; and Chirp Form create/edit/delete with species tags and public/flock-only fields.
>
> Implement safe single-photo validation/re-encoding and protected image serving. Demonstrate persisted posts and tags after restart and upload/ownership tests. Use fixture flock memberships until the full flock UI arrives in Checkpoint 5.

## Implementation evidence

| Deliverable | Local implementation evidence |
| --- | --- |
| Species browsing and selection | `species.go`, `templates/species.html`: common/scientific-name search, quick-access sidebar, catalog cards, `?selected=<id>` detail in the same layout. |
| Seeded reference photos and source links | Twelve locally bundled Wikimedia JPEGs and `static/images/species/credits.json`; `seeds.go` backfills existing catalogs; migration 003 adds photo credits. Photo source, author, license, and field-guide source links are displayed. README records every image license. |
| Tagged visible chirps | `Store.VisibleChirps` selects tagged posts and applies current membership filtering before pagination; species pagination retains selection and catalog search. |
| Paginated Feed | `/feed?page=<n>`, 20 visible posts per page, stable newest-first timestamp/ID order, author/species/flock links, author controls, and empty states. |
| Chirp create/edit/delete | `/chirps/form`, optionally `?id=<id>`; POST to save; confirmed POST to `/chirps/:id/delete`. `chirps.go`, `templates/chirp-form.html`, `templates/chirps.html`. |
| Species tags and visibility fields | Multiple optional species tags, public/flock visibility, optional flock destination, current membership validation, and transactional post/tag writes. |
| Safe single-photo uploads | `photos.go`: one actual JPEG/PNG, 5 MiB, 3000 × 3000 pixels, permission confirmation, decode/re-encode, random filename, mode 600, private directory. Multipart requests retain CSRF and a 6 MiB request bound. |
| Protected photo serving | `/chirps/:id/photo` queries current visibility; hidden/missing photos return 404. No public upload directory; private/no-store and nosniff headers; unsafe paths and symlinks rejected. |
| Persisted posts and tags after restart | `TestChirpLifecycleAndPersistence` closes/reopens SQLite and builds a new HTTP server; saved edits, replaced tags, photo bytes, and the existing session remain available. |
| Upload and ownership tests | `chirps_test.go`, `photos_test.go`: validation, author-only writes, CSRF, rollback, photo limits/formats/metadata, replacement/removal/deletion, and failed-save cleanup. |
| Fixture flock memberships | `fixtureFlock` and test memberships exercise eligible/guest/nonmember visibility, direct image requests, membership removal, and public/private transitions. No demonstration accounts or flocks are inserted into normal application data. |

## Behavior and constraints

Chirps accept 1–500 Unicode characters; content remains plain text and Go templates escape it. Tags must reference existing catalog entries; repeated tags are deduplicated. Public chirps can optionally belong to one of the author's flocks. Flock-only chirps require a current flock membership and a selected flock. Validation errors preserve text and valid selections; uploaded files must be selected again, as required by browser file controls.

Post and tag writes share a transaction. File creation happens before the save; failures remove the new file. Replacement, removal, and chirp deletion remove old photos only after successful commit. A text-only edit keeps the current photo from the database. Existing account deletion also removes uploaded chirp photos after its transaction commits.

Feed, selected-species results, and image requests all apply the same visibility predicate: a post is public or the viewer currently belongs to its flock. Filtering happens before LIMIT/OFFSET. The profile owner sees only public chirps on their public profile, as before. Photos are looked up by post ID, never a caller-supplied storage filename. Every image request rechecks membership and current visibility, including after a member leaves or the author changes a post's visibility.

The flock and observation-search interfaces remain scheduled for Checkpoints 5 and 6. Current flock memberships are supported by the database and the chirp form; tests supply fixture memberships without shipping fake production content.

## Verification and demonstration

Local verification on November 3, 2026 with Go 1.22.2 passed the complete Go suite, the complete suite with the race detector, Go vet, shell syntax checks, and Git whitespace checks. Integration tests exercised real HTTP handlers against temporary SQLite databases and private temporary upload directories. The persistence test closed and reopened the database before restoring the session and checking saved content, tags, and the photo route.

A fresh clone of implementation commit `f655ea8` started using only `./start.sh`, empty Go module/build caches, no copied `.env`, and no existing database or uploads. The script downloaded dependencies, generated private configuration, initialized SQLite, and served the app. Real HTTP checks passed for all twelve catalog photos and attribution, species search/selection, chirp create/edit/tag replacement, Feed/Species pagination, public/flock-only visibility, author permissions, CSRF, and retained validation errors. A JPEG upload larger than 1 MiB was decoded and re-encoded without its trailing metadata marker. After terminating and restarting the actual server process through `start.sh`, posts, edited tags, photos, and existing authenticated sessions persisted. Further checks passed for membership revocation, visibility transitions, confirmed deletion, and complete scoped fixture cleanup; SQLite reported no foreign-key violations.

Run the complete suite and static checks:

```bash
go test ./...
go test -race ./...
go vet ./...
bash -n start.sh deploy/setup-ec2.sh deploy/deploy.sh
git diff --check
```

Focused automated demonstrations:

```bash
go test -v -run 'TestChirpLifecycleAndPersistence|TestChirpValidationAndPermissions'
go test -v -run 'TestFeedSpeciesVisibilityAndPagination|TestPhoto'
```

For a browser demonstration, start with `./start.sh`, register a member, browse/search the species catalog, and open a species card. Publish a tagged chirp with a photo, follow its species links, edit its text/tags/photo, then stop and restart the app. Confirm the existing session, saved post, tags, and photo persist. Try editing through a different account and show the ownership rejection. Delete through the author's confirmed action and show that the photo route returns 404. The fixture tests demonstrate flock-only behavior until flock management is implemented.

The release procedure is to tag the reviewed source as `cp4`, push the tag, copy the updated `deploy/` directory to EC2 and rerun setup to raise Nginx's request limit to 6 MiB, then deploy with `DEPLOY_TAG=cp4`. Verify the public health response's `X-Avian-Revision` matches the tagged commit and repeat the HTTP checks around an actual service restart. The checkpoint post must use `git diff --shortstat cp3 cp4` and cite the tagged files and live routes. Record the Panopto video against that same deployed revision. Existing Checkpoint 3 post/video drafts are preserved.
