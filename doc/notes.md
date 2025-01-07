# Notes

## Signal dynamic range compression

### Current variable pulse frequency version

| Function | Scale  | Comments                                                 |
|----------|--------|----------------------------------------------------------|
| Exp 0.46 | 0.036  | Less dynamic range but more feedback, small bumps too    |
|          |        | strong compared to steering feedback, missing low freq   |
| Exp 0.46 | 0.0355 | Less dynamic range but more feedback, small bumps harsh  |
|*Exp 0.46 | 0.0354 | Less dynamic range but more feedback, slightly harsh     |
|*Exp 0.46 | 0.0353 | Less dynamic range but more feedback, small bumps about  |
|          |        | right with lower frequencies present                     |
|*Exp 0.46 | 0.0352 | Less dynamic range but more feedback, slightly weak      |
| Exp 0.46 | 0.035  | Less dynamic range but more feedback, small bumps a bit  |
|          |        | too weak compared to steering feedback                   |
| Exp 0.50 | 0.03   | Good range, maybe a bit harsh for smaller bumps          |
|*Exp 0.50 | 0.028  | Good range, smaller bumps fairly subtle                  |
| Exp 0.55 | 0.02   | reasonably good, small bumps still a bit too soft        |
| Exp 0.56 | 0.02   | reasonably good, small bumps still a bit too soft        |
| Exp 0.56 | 1/54   | nuanced small bumps, lacking impact on ripple strips     |

## Older fixed pulse frequency version

| Function | Scale | Comments                                                 |
|----------|-------|----------------------------------------------------------|
| Exp 0.50 | 1/57  | small bumps slightly too loud                            |
| Log10    | 0.08  | small bumps too loud                                     |
| Log2     | 0.025 | small bumps too loud                                     |
