# How Surge Optimizes Downloads

Surge is designed to maximize download speeds by overcoming the limitations of standard browser downloads.

## The Problem with Browser Downloads

A standard browser usually opens one HTTP connection to the server.
A server usually limits the bandwidth it gives to a single connection to make it fair for all users.
Download managers (like Surge) open up a lot of requests at once (32 in Surge). They use it to split the file into a lot of small parts and download those parts individually.

## Connection Variability

Now all connections are also not created equal, there are fast connections and slow connections. because of stuff like load balancers and CDN's and stuff. Download managers have a bunch of ways to optimize these connections.

## The Top 3 Optimizations in Surge

1.  **Split the largest chunk whenever possible:** This ensures we don't have idle workers.
2.  **Smart "Work Stealing":** Near the end, when fast workers are done and slow workers are still doing their work, we make the fast idle workers "steal work" from the slow workers.
3.  **Slow Worker Restart:** We find the mean speed of all workers. If there is a worker performing less than 0.3x of mean, we restart it in the hopes that it will get a better pathway to the server which will be faster.
