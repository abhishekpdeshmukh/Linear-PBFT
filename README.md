# Linear-PBFT Banking Application

## Overview
This project implements a distributed banking application using a variant of the Practical Byzantine Fault Tolerance (PBFT) protocol called **Linear-PBFT**. Designed for fault-tolerant environments, Linear-PBFT achieves consensus with a reduced communication complexity, making it suitable for systems requiring efficient yet secure transaction handling across multiple nodes.

## Features
- **Distributed Consensus**: Implements the Linear-PBFT protocol for consensus with linear communication complexity in the normal case.
- **Resilient Banking Transactions**: Processes client requests with consensus-driven transactions, ensuring all nodes maintain accurate state despite potential faults.
- **Fault Tolerance**: Withstands Byzantine faults, supporting up to 2 faulty nodes out of 7 (3f + 1).
- **CLI Utilities**: Commands like `PrintBalance`, `PrintLog`, `PrintDB`, and `Performance` allow insights into node states, transaction logs, and overall system performance.
