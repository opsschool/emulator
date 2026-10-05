#!/usr/bin/env bash
# Reference mitigation: kill the waiting ALTER. Queries on orders run again,
# but the migration hasn't happened and 2.4.0 keeps leaving transactions
# open, so the next schema change will hang the same way.
set -euo pipefail
for id in $(mysql -N -B -e "SELECT id FROM information_schema.processlist
    WHERE state = 'Waiting for table metadata lock' AND info LIKE 'ALTER TABLE%'"); do
  mysql -e "KILL $id"
done
