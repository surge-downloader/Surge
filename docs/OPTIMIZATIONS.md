# How Surge Optimizes Downloads

Surge is designed to maximize download speeds by overcoming the limitations of standard browser downloads.

## The Problem with Browser Downloads

A standard browser usually opens one HTTP connection to the server.
A server usually limits the bandwidth it gives to a single connection to make it fair for all users.
Download managers (like Surge) open up a lot of requests at once (32 in Surge). They use it to split the file into a lot of small parts and download those parts individually.

## Connection Variability

Now all connections are also not created equal, there are fast connections and slow connections. because of stuff like load balancers and CDN's and stuff. Download managers have a bunch of ways to optimize these connections.

## The Top Optimizations in Surge

1.  **Large chunks:** We split the download into $num_workers parts and give each worker $(fileSize/numworkers) bytes to download. This ensures that each connection is large and efficient.
2.  **HealthCheck:** We find the mean speed of all workers. If there is a worker performing less than 0.3x of mean, we restart it in the hopes that it will get a better pathway to the server which will be faster.
3.  **StealWork:** Near the end, when fast workers are done and slow workers are still doing their work, we make the fast idle workers "steal work" from the slow workers.
4.  **HedgeWork:** Near the end, when there are idle workers and active workers and the StealWork is not possible (chunk size becomes too small), we make an idle worker do the same task as the active worker. If the idle worker finishes first, we use its result and cancel the active worker's task.
