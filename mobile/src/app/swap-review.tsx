import { ComingSoon } from '../components/ui';

// From / to, rate, fee, countdown ring from the quote's expires_at, slide to
// confirm. POST /v1/swaps/quotes then POST /v1/swaps
export default function SwapReview() {
  return <ComingSoon sheet title="Review swap" milestone="M2" />;
}
