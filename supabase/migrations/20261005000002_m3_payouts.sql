-- M3: payouts. Rails come from Bitnob's live corridor list (bank,
-- mobile_money, paybill, paytill, ...), so they are stored as text rather
-- than a fixed enum.

alter table beneficiaries alter column rail type text using rail::text;
drop type payout_rail;

-- Why a payout did not complete, for support and the transaction screen.
alter table payouts add column failure_reason text;
alter table payouts add column payment_reason text;
