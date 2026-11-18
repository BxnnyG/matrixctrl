import { configure } from "@testing-library/react";

// findBy*/waitFor give up after 1 s by default. On the build host the suite shares six
// cores with k3s; under that load two tests that pass alone (SelfUpdate, helm/upgrade)
// timed out in `make check` — a red check that said nothing about the code. A real
// failure still fails, only five seconds later (etappe 124).
configure({ asyncUtilTimeout: 5000 });
