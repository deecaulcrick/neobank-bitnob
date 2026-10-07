# Loads backend/.env for the backend targets.
ifneq (,$(wildcard backend/.env))
include backend/.env
export
endif

.PHONY: api worker whoami test test-db migrate mobile invite block

api:            ## run the JSON API + webhook receiver
	cd backend && go run ./cmd/api

worker:         ## run the webhook processor, sweeper and reconciler
	cd backend && go run ./cmd/worker

whoami:         ## M0 check: signed GET /api/whoami against Bitnob
	cd backend && go run ./cmd/whoami

test:           ## unit tests (ledger DB tests are skipped)
	cd backend && go vet ./... && go test ./...

test-db:        ## all tests, including ledger tests, against TEST_DATABASE_URL
	cd backend && go test -count=1 ./...

migrate:        ## push supabase/migrations using DATABASE_URL from backend/.env
	supabase db push --db-url "$(DATABASE_URL)"

mobile:         ## start the Expo dev server
	cd mobile && npx expo start

invite:         ## closed beta: allow a phone number to sign up (PHONE=2348012345678)
	@test -n "$(PHONE)" || (echo "usage: make invite PHONE=2348012345678" && exit 1)
	psql "$(DATABASE_URL)" -c "insert into beta_invites (phone) values (regexp_replace('$(PHONE)', '[^0-9]', '', 'g')) on conflict do nothing"

block:          ## refuse payouts to a recipient name (NAME="Full Name")
	@test -n "$(NAME)" || (echo 'usage: make block NAME="Full Name"' && exit 1)
	psql "$(DATABASE_URL)" -c "insert into screening_blocklist (name, name_key) select n, (select string_agg(w, ' ' order by w) from regexp_split_to_table(trim(regexp_replace(upper(n), '[^A-Z0-9]+', ' ', 'g')), ' ') w) from (select '$(NAME)'::text as n) s on conflict do nothing"
