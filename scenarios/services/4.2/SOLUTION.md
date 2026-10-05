# Retry storm against a rate-limited gateway

## What happened

Two changes went out in the morning. Each was safe on its own.

1. The payments team limited the gateway to two authorizations at a time
   (PAY-298), because the card network allows no more per merchant, and
   made it as slow as production, 100 to 180ms per authorization. That's
   about 14 authorizations a second. The shop takes about 10 orders a
   second at peak and under 3 the rest of the time, so there was room.
2. The shop's payment client was changed to "fail fast and retry"
   (PAY-311): it gives up on a call after 500ms instead of 2s, and retries
   6 to 10 times instead of twice, at once instead of after a growing
   pause, with no limit on the total. Every order still has 5 seconds in
   all.

At the next traffic peak the gateway's line grew enough that a few
authorizations waited more than 500ms. The shop gave up on them and sent
them again. The gateway, like many services, doesn't notice that a caller
has gone: it still works through every request it has accepted. So each
timeout added work, the line grew, and more calls timed out. Once the wait
was longer than 500ms, every call timed out, and each order turned into 7
or more calls, all of them wasted. That's 18 or more calls a second at
normal traffic, more than the gateway can do, so the line never shrank and
the overload kept itself going after the peak ended. This is sometimes
called a metastable failure: the trigger is gone, but the system stays
broken.

The signs: payments calls a second many times the order rate
(`shop_payment_requests_total` against `POST /orders`), thousands of
authorizations waiting in the gateway's log, and orders that fail after
about 5 seconds.

## Mitigate

Stop the extra work and empty the line. Either:

- turn the shop's retries down or off (`SHOP_RETRY_MAX=0` in
  `/etc/shop/shop.env`), restart `shop.service`, and restart
  `shop-payments.service` to drop the queued work, or
- with the payments team's agreement, lift the gateway's limit for the
  moment and restart it.

Restarting the gateway alone works only until the next traffic peak.

## Fix

Put the shop's retry policy back to one the gateway can survive: a 2s
timeout and two retries with backoff, so a slow gateway sees at most three
calls per order inside the 5 second deadline. Keep the gateway's limit; it belongs to
the card network. Better still:

- give retries a budget, for example no more than 10% extra calls, so a
  slow dependency gets less traffic, not more;
- send the remaining deadline with each call, so the gateway can drop work
  nobody is waiting for;
- load test changes to timeouts and retries against the dependency's real
  limits.

## Curriculum

- [Architecture 201: timeouts and retries](https://www.opsschool.org/architecture_201.html#timeouts-and-retries)
