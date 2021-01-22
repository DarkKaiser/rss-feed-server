pipeline {
    
    agent any

    environment {
        PROJECT_NAME = "RSS Feed 서버"
        TELEGRAM_CHAT_ID = credentials('telegramChatId')
    }

    stages {

        stage('준비') {
            steps {
                script {
                    try {
                        sh "rm ./rss-feed-server"
                    } catch (e) {
                    }
                }
            }
        }

        stage('체크아웃') {
            steps {
                checkout([$class: 'GitSCM', branches: [[name: '*/main']], doGenerateSubmoduleConfigurations: false, extensions: [], submoduleCfg: [], userRemoteConfigs: [[credentialsId: 'github-darkkaiser-credentials', url: 'https://github.com/DarkKaiser/rss-feed-server.git']]])
            }
        }

        stage('빌드') {
            steps {
                sh "./build_raspberrypi.sh"
            }
        }

        stage('배포') {
            steps {
                sh '''
                    sudo cp -f ./rss-feed-server /usr/local/rss-feed-server/
                    sudo cp -f ./rss-feed-server.운영.json /usr/local/rss-feed-server/rss-feed-server.json
                    sudo chown pi:staff /usr/local/rss-feed-server/rss-feed-server
                    sudo chown pi:staff /usr/local/rss-feed-server/rss-feed-server.json
                '''
            }
        }

        stage('서버 재시작') {
            steps {
                sh "sudo /usr/local/rss-feed-server/rss-feed-server-restart.sh"
            }
        }

    }

    post {
        success {
            script {
                echo 'success'
                echo env.TELEGRAM_CHAT_ID
                echo '【 Jenkins 알림 > ' + env.PROJECT_NAME + ' 】\n\n빌드 작업이 성공하였습니다.\n\n' + env.BUILD_URL
                telegramSend(message: '【 Jenkins 알림 > ' + env.PROJECT_NAME + ' 】\n\n빌드 작업이 성공하였습니다.\n\n' + env.BUILD_URL, chatId: 297396697)
            }
        }
        failure {
            script {
                echo 'failure'
                telegramSend(message: '【 Jenkins 알림 > ' + env.PROJECT_NAME + ' 】\n\n빌드 작업이 실패하였습니다.\n\n' + env.BUILD_URL, chatId: env.TELEGRAM_CHAT_ID)
            }
        }
    }

}
