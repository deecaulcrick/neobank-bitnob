-- M1: onboarding. Bitnob requires an email to create a customer, and one BVN
-- may only ever back one of our users.

alter table users add column email extensions.citext unique;

-- id_reference holds a keyed hash of the BVN, never the BVN itself.
create unique index kyc_records_one_owner_idx
  on kyc_records (id_type, id_reference) where status = 'approved';
