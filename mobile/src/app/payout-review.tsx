import { ComingSoon } from '../components/ui';

// Amount out, fee, rate, arrival estimate, countdown. Shows "Sending" until
// the rail confirms, never "Sent" early.
export default function PayoutReview() {
  return <ComingSoon title="Review payout" milestone="M3" />;
}
