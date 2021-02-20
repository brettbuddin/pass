upstream "accounts" {
    destination = "http://basic.local" 
    owner = "Identity <team-identity@company.com>"

    route {
        methods = ["GET"]
        path = "/accounts"
    }
}
