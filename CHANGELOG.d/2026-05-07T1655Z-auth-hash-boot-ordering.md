- Documented the boot-then-listen happens-before edge at `auth.Hash`'s
  read of the package-level `BcryptCost`. The contract was already
  spelled out from the writer side (`SetBcryptCost`); this mirrors it
  on the reader side so a future caller added before the HTTP listener
  opens — or a second writer — surfaces the invariant instead of a
  silent race. Comment-only; no behavior change. Closes #829.
